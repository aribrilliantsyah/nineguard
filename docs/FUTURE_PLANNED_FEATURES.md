# Future Planned Features

Planned enhancements discussed for post-plugin roadmap.

---

## 1. Image Detection & Multimodal Logging

### Goal
Track and flag requests containing images (multimodal prompts) across agents and tools.

### Mechanism
- **Inspection**: Parse incoming chat completion body in `proxy.go`. Check `messages[].content` array for items where `type == "image_url"`.
- **Database Schema**:
  - `ALTER TABLE traffic_logs ADD COLUMN has_images INTEGER DEFAULT 0;`
  - `ALTER TABLE traffic_logs ADD COLUMN image_count INTEGER DEFAULT 0;`
- **Telemetry & UI**:
  - Traffic Explorer row badge: 🖼️ icon showing image count (e.g. `🖼️ 2 img`).
  - Traffic filter: `has_images=1` (filter multimodal requests only).
  - Detail modal: displays attached image count and formats.

---

## 2. Heavy Token Usage Flagging (Spike Alerts)

### Goal
Highlight individual requests that consume massive context or spike unexpectedly.

### Specification & Decisions
- **Scope**: Global system setting `heavy_token_threshold` in `settings` table (default: `8000` tokens, user-adjustable in UI).
- **Evaluation**: On request completion in `proxy.go`, check `total_tokens >= heavy_token_threshold`.
- **Action**: Advisory only. Never blocks or rejects a request.
- **Telemetry & UI**:
  - Traffic Explorer: warning badge ⚠️ `Heavy (<N>k tok)`. Filter option `heavy_only=1`.
  - System Logs: emits `WARN` log entry (`source=traffic`, e.g. `token_spike: key "pi-dev" consumed 18,400 tokens`).

---

## 3. Token Quotas & Limiting per API Key

### Goal
Enforce daily, weekly, monthly, or lifetime token budgets per client API key without deleting or invalidating the credential.

### Specification & Decisions (see ADR-0005)
- **Database Schema**:
  - `ALTER TABLE api_keys ADD COLUMN quota_limit INTEGER DEFAULT 0;` (0 = unlimited)
  - `ALTER TABLE api_keys ADD COLUMN quota_period TEXT DEFAULT 'none';` (`none`, `daily`, `weekly`, `monthly`, `total`)
- **Enforcement (Soft Post-Facto)**:
  - In `proxy.go`, before forwarding, compute tokens consumed in current window via SQLite index `idx_traffic_key_id_ts(api_key_id, timestamp)`:
    - `daily`: `timestamp >= datetime('now', 'start of day')` (UTC midnight)
    - `weekly`: Monday 00:00 UTC (ISO 8601)
    - `monthly`: 1st of month 00:00 UTC
    - `total`: all-time sum
  - If limit reached: return **HTTP 429 Too Many Requests**:
    - Header: `Retry-After: <seconds_until_reset>`
    - Body:
      ```json
      {
        "error": {
          "message": "API key token quota exceeded (120,000 / 100,000 tokens daily). Resets in 3h 42m (at 00:00 UTC).",
          "type": "insufficient_quota",
          "code": "quota_exceeded"
        }
      }
      ```
  - Automatically unblocks when next period begins or administrator raises quota limit. Key is never deleted.
- **UI**:
  - Key modal: inputs for `Quota Limit` and `Quota Period`.
  - Endpoints table: quota consumption progress bar (e.g. `340k / 500k (68%)`). Red highlight when quota exhausted.
  - Reset times rendered in viewer's local timezone (e.g. WIB) using browser context.

---

## 4. Token Burn Rate & Velocity Baselines

### Goal
Provide historical burn-rate context in the UI so administrators know what quota limits to set instead of guessing.

### Mechanism
- **Key Modal Helper**:
  - When editing a key, calculate and show past usage velocity pills next to the quota input:
    - `Past 24h: 38k tokens`
    - `7d avg: 45k tokens/day`
    - `30d total: 1.2M tokens`
  - Quick-preset buttons: `Set 1.5x daily avg (68k)` or `Set 2x daily avg (90k)`.
- **Endpoints & Keys Table**:
  - Display burn velocity below total tokens: e.g. `45.1M total (~120k/day)`.
- **Usage Reports View**:
  - System-wide burn rate metric: e.g. `~450k tokens/day across all keys`.

---

## 5. Secret Leak Guardrail & Sensitive Data Redaction

### Goal
Prevent client coding agents from accidentally leaking credentials, private keys, environment secrets, and sensitive tokens to upstream LLM providers.

### Architecture Decision
Implemented as a **Built-in Plugin** (`internal/plugins/builtin/secretguard`), integrating with NineGuard's plugin pipeline rather than hardcoding in core proxy. Supports standard plugin precedence, bindings (Global/ModelGroup/Key), and failure policy (`fail-closed` for security).

### Mechanism
- **Inspection**: Pre-flight regex scanning on incoming request body in plugin `Transform()` before forwarding to upstream.
  - Matches common credential signatures:
    - Private keys (`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
    - High-entropy API tokens (`sk-[a-zA-Z0-9_-]{20,}`, `ghp_[a-zA-Z0-9]{36}`, `AKIA[0-9A-Z]{16}`)
    - Common environment formats (`DATABASE_URL=...`, `AWS_SECRET_ACCESS_KEY=...`)
- **Action Modes**: System or per-key setting `secret_guard_action`:
  - `block`: Rejects request immediately with **HTTP 400 Bad Request** / descriptive error payload without exposing the matched secret.
  - `redact`: Replaces matched substring with `[REDACTED_SECRET:<type>]` in-flight before forwarding to provider.
  - `warn_only`: Emits security audit log while allowing request to proceed.
- **Telemetry & UI**:
  - Security audit badge in Traffic Explorer: 🛡️ `Secret Blocked` or `Redacted`.
  - System Logs: emits `SECURITY_ALERT` entry with client key ID and rule matched (no secret values stored in logs).
  - Settings page: toggleable rulesets and custom regex pattern definitions.

---

## 6. Payload Inspector & Replay Request in Traffic Explorer

### Goal
Provide granular request/response body visibility for debugging agent tool calls, schema issues, and hallucinations, with the ability to replay requests.

### Mechanism
- **Storage Strategy**:
  - Optional setting `record_payloads`: `disabled`, `errors_only`, or `all`.
  - Stored in a separate table with foreign key to `traffic_logs` to preserve lightweight traffic listing:
    - `CREATE TABLE traffic_payloads (traffic_id INTEGER PRIMARY KEY, request_body BLOB, response_body BLOB, created_at DATETIME, FOREIGN KEY(traffic_id) REFERENCES traffic_logs(id) ON DELETE CASCADE);`
  - Payloads optionally compressed (gzip/zstandard) to preserve disk space.
  - Automated TTL cleanup worker (e.g. purges payloads older than 7 days).
- **Telemetry & UI**:
  - Detail modal in Traffic Explorer:
    - New tabs: **Messages / Prompt**, **Response Body**, and **Tool Calls**.
    - Formatted JSON tree viewer with search & syntax highlighting.
  - Action buttons:
    - **Copy as cURL**: Generate reproducible cURL command including upstream headers.
    - **Replay Request**: Open a playground modal to resend the payload directly to the original or alternative model and inspect differences.

