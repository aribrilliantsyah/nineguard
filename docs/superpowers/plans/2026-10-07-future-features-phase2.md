# Phased Implementation Plan: Multimodal Logging, Velocity Baselines, Secret Guardrail & Payload Inspector

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Implement the remaining four post-plugin roadmap capabilities from `docs/FUTURE_PLANNED_FEATURES.md`:
1. **Multimodal Logging & Image Detection (§1):** Detect image inputs in chat completion requests, record `has_images` and `image_count` in `traffic_logs`, and surface `🖼️ N img` badges and filters in Traffic Explorer.
2. **Token Burn Rate & Velocity Baselines (§4):** Compute 24h, 7d daily average, and 30d usage velocities per API key to provide context pills and preset multipliers in the Quota modal.
3. **Secret Leak Guardrail (§5):** Implement a built-in security plugin (`secretguard`) that scans prompts for API keys, private keys, and environment secrets with configurable `block` and `redact` actions.
4. **Payload Inspector & Request Replay (§6):** Store request and response bodies in `traffic_payloads` (disabled/errors_only/all, 512KB cap, 7d TTL), display formatted payload tabs in Traffic Explorer, and provide cURL copy and replay playground.

---

## Global Constraints

- Go `1.27.1`; all code must pass `CGO_ENABLED=0 go test -tags server ./...`.
- Database schema changes in `internal/db/db.go` must be backward-compatible with existing SQLite databases via `ALTER TABLE` and `CREATE TABLE IF NOT EXISTS`.
- Secret Guardrail must be implemented as a **Built-in Plugin** (`internal/plugins/builtin/secretguard.go`) conforming to the NineGuard plugin interface (`Plugin`, `Transform()`, `Rejection`), never hardcoded in `proxy.go`.
- Payload storage defaults to `disabled`. When enabled, bodies are strictly capped at 512KB each.
- Automated TTL background worker runs every 12 hours to delete payloads older than 7 days without locking SQLite writes.
- Vanilla JavaScript (ES modules) with no Node build step; frontend unit tests in `web/jstest/` run with `node --test`.

---

## File Map

| File | Subsystem | Responsibility |
|---|---|---|
| `internal/db/db.go` | All | Migrations: `traffic_logs.has_images`, `traffic_logs.image_count`, `traffic_payloads` table, default settings |
| `internal/db/features_migration_test.go` (new) | DB | Verify columns, tables, and indexes for all 4 features |
| `internal/proxy/proxy.go` | Multimodal / Payloads | Image detection in chat body; conditional payload capture; `secretguard` execution via plugin engine |
| `internal/traffic/multimodal_test.go` (new) | Multimodal | Unit tests for image detection & traffic log recording |
| `internal/traffic/traffic.go` | Multimodal / Velocity / Payloads | `LogEntry` multimodal fields, filter by image; `GetVelocityStats`; payload CRUD & TTL cleanup |
| `internal/traffic/velocity_test.go` (new) | Velocity | Unit tests for 24h, 7d avg, 30d token velocity calculation |
| `internal/traffic/payloads_test.go` (new) | Payloads | Unit tests for payload storage, 512KB cap, and TTL eviction |
| `internal/plugins/builtin/secretguard.go` (new) | Security Plugin | Regex scanning for credentials, `block` vs `redact` modes |
| `internal/plugins/builtin/secretguard_test.go` (new) | Security Plugin | Unit tests for token regexes, redaction replacements, and rejections |
| `internal/handler/handler.go` | API | Handlers for `/keys/{id}/velocity`, `/traffic/{id}/payload`, `/settings/payloads` |
| `internal/handler/features_handler_test.go` (new) | API | Tests for velocity endpoint, payload retrieval, and settings |
| `web/static/js/views/traffic.js` | UI | `🖼️ N img` badge, `has_images` filter chip; payload tabs in detail modal; cURL copy & replay |
| `web/static/js/views/endpoints.js` | UI | Velocity context pills in Quota modal (`Past 24h`, `7d avg`) and preset multipliers |

---

# Part 1: Image Detection & Multimodal Logging (Feature 1)

### Task 1: Database Schema & Multimodal Log Columns

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/multimodal_migration_test.go`

- [x] **Step 1: Write failing test**
  Verify `has_images` and `image_count` columns exist on `traffic_logs`.
- [x] **Step 2: Run test to verify failure**
  `go test ./internal/db/ -run TestMultimodalColumns -v`
- [x] **Step 3: Implement migration**
  In `internal/db/db.go`:
  - Add `has_images INTEGER DEFAULT 0` and `image_count INTEGER DEFAULT 0` to `CREATE TABLE IF NOT EXISTS traffic_logs`.
  - Add `ALTER TABLE traffic_logs ADD COLUMN has_images INTEGER DEFAULT 0;`
  - Add `ALTER TABLE traffic_logs ADD COLUMN image_count INTEGER DEFAULT 0;`
- [x] **Step 4: Run test to verify pass**
- [x] **Step 5: Commit**
  `git commit -m "feat(db): add has_images and image_count columns to traffic_logs"`

### Task 2: Multimodal Body Inspection in Proxy & Traffic Logging

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/traffic/traffic.go`
- Create: `internal/traffic/multimodal_test.go`

- [x] **Step 1: Write failing test**
  Test request with `messages[].content` containing `[{"type":"text"},{"type":"image_url"}]`. Verify `has_images=true` and `image_count=1`.
- [x] **Step 2: Run test to verify failure**
- [x] **Step 3: Implement image inspection**
  In `internal/proxy/proxy.go`:
  - When parsing `chatRequest`, inspect `messages[].content` for items where `type == "image_url"` or `type == "input_image"` or object has `image_url`.
  - Pass `HasImages: count > 0` and `ImageCount: count` into `traffic.LogEntry`.
  In `internal/traffic/traffic.go`:
  - Add `HasImages bool` and `ImageCount int` to `LogEntry`.
  - Update `Record`, `QueryLogs`, `FilterParams` (`HasImages *bool`).
- [x] **Step 4: Run test to verify pass**
- [x] **Step 5: Commit**
  `git commit -m "feat(traffic): record multimodal image detection in traffic logs"`

### Task 3: Multimodal Badge & Filter in Traffic Explorer UI

**Files:**
- Modify: `web/static/js/views/traffic.js`

- [x] **Step 1: Add row badge**
  In `traffic.js` row renderer, if `e.has_images` is true, render badge `🖼️ ${e.image_count > 1 ? e.image_count + ' imgs' : 'img'}` in `.c-model`.
- [x] **Step 2: Add filter chip**
  Add filter chip `🖼️ Images` to toggle `has_images=1`.
- [x] **Step 3: Commit**
  `git commit -m "feat(ui): add multimodal image badge and filter in traffic explorer"`

---

# Part 2: Token Burn Rate & Velocity Baselines (Feature 4)

### Task 4: Velocity Stats Query (`internal/traffic`)

**Files:**
- Modify: `internal/traffic/traffic.go`
- Create: `internal/traffic/velocity_test.go`

- [x] **Step 1: Write failing test**
  Test `GetVelocityStats(apiKeyID string)` with mock traffic inserted across past 24 hours, past 7 days, and past 30 days. Verify `Tokens24h`, `Tokens7dAvg`, `Tokens30dTotal`.
- [x] **Step 2: Run test to verify failure**
- [x] **Step 3: Implement `GetVelocityStats`**
  In `internal/traffic/traffic.go`:
  ```go
  type VelocityStats struct {
      Tokens24h     int64 `json:"tokens_24h"`
      Tokens7dAvg   int64 `json:"tokens_7d_avg"`
      Tokens30d     int64 `json:"tokens_30d"`
  }
  ```
  Query SQLite using `idx_traffic_key_id_ts`:
  - `tokens_24h`: `timestamp >= datetime('now', '-24 hours')`
  - `tokens_7d`: `timestamp >= datetime('now', '-7 days')` / 7
  - `tokens_30d`: `timestamp >= datetime('now', '-30 days')`
- [x] **Step 4: Run test to verify pass**
- [x] **Step 5: Commit**
  `git commit -m "feat(traffic): calculate token burn velocity stats per api key"`

### Task 5: Velocity API Endpoint & Key Modal UI

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `web/static/js/views/endpoints.js`
- Create: `internal/handler/velocity_handler_test.go`

- [x] **Step 1: Write failing test**
  Test `GET /api/v1/keys/{id}/velocity` returns JSON matching `VelocityStats`.
- [x] **Step 2: Implement handler**
  Add `h.GetKeyVelocity` and route `GET /api/v1/keys/{id}/velocity`.
- [x] **Step 3: Update Key modal in `endpoints.js`**
  When editing an existing key, fetch `/keys/{id}/velocity`. Render pills below Quota Limit:
  - `Past 24h: 38k tokens`
  - `7d avg: 45k tokens/day`
  - `30d: 1.2M tokens`
  - Clickable presets: `[1.5x Daily Avg]` (sets quota limit to 1.5 * 7d avg), `[2x Daily Avg]`.
- [x] **Step 4: Run tests and commit**
  `git commit -m "feat(ui): add token burn velocity pills and quota presets in key modal"`

---

# Part 3: Secret Leak Guardrail Plugin (Feature 5)

### Task 6: Built-in `secretguard` Plugin Engine

**Files:**
- Create: `internal/plugins/builtin/secretguard.go`
- Create: `internal/plugins/builtin/secretguard_test.go`
- Modify: `internal/db/db.go` (seed plugin record)

- [x] **Step 1: Write failing test**
  Test `ApplySecretGuard`:
  - Matches OpenAI API key `sk-...`
  - Matches GitHub PAT `ghp_...`
  - Matches RSA/EC private key `-----BEGIN PRIVATE KEY-----`
  - Matches AWS access key `AKIA...`
  - Mode `block`: returns `*plugins.Rejection` (HTTP 400).
  - Mode `redact`: returns modified body with `[REDACTED_SECRET:<type>]`.
  - Mode `warn_only`: passes unmodified with security warning flag.
- [x] **Step 2: Run test to verify failure**
- [x] **Step 3: Implement `secretguard` plugin**
  - Implement `ApplySecretGuard(body []byte, settings map[string]any) ([]byte, *plugins.Rejection, error)`.
  - Seed plugin in `internal/db/db.go`:
    ```sql
    INSERT OR IGNORE INTO plugins (id, name, description, category, kind, failure_policy, default_settings)
    VALUES ('secretguard', 'Secret Guardrail', 'Scans incoming prompts for credentials, API tokens, and private keys.', 'security', 'builtin', 'fail_closed', '{"action":"block"}');
    ```
- [x] **Step 4: Run test to verify pass**
- [x] **Step 5: Commit**
  `git commit -m "feat(plugins): implement built-in secretguard security plugin"`

### Task 7: Integrate `secretguard` in Plugin Pipeline & Telemetry

**Files:**
- Modify: `internal/plugins/pipeline.go`
- Modify: `internal/plugins/builtin/common.go`
- Create: `internal/plugins/secretguard_pipeline_test.go`

- [x] **Step 1: Write failing test**
  Verify `secretguard` executes in `PipelineExecutor.Execute`, rejects on leak when action is `block`, and redacts in-flight when action is `redact`.
- [x] **Step 2: Implement execution dispatch**
  Register `secretguard` in builtin plugin dispatcher.
- [x] **Step 3: Run tests and commit**
  `git commit -m "feat(plugins): wire secretguard into plugin execution pipeline"`

---

# Part 4: Payload Inspector & Replay in Traffic Explorer (Feature 6)

### Task 8: `traffic_payloads` Schema & Storage Engine

**Files:**
- Modify: `internal/db/db.go`
- Modify: `internal/traffic/traffic.go`
- Create: `internal/traffic/payloads_test.go`

- [x] **Step 1: Write failing test**
  Test `SavePayload(trafficID, reqBody, respBody)` with 512KB size clamping.
  Test `GetPayload(trafficID)`.
  Test `PurgeOldPayloads(olderThan time.Duration)`.
- [x] **Step 2: Implement schema and storage**
  In `internal/db/db.go`:
  ```sql
  CREATE TABLE IF NOT EXISTS traffic_payloads (
      traffic_id INTEGER PRIMARY KEY,
      request_body BLOB,
      response_body BLOB,
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
      FOREIGN KEY(traffic_id) REFERENCES traffic_logs(id) ON DELETE CASCADE
  );
  INSERT OR IGNORE INTO settings (key, value) VALUES ('record_payloads', 'disabled');
  ```
  In `internal/traffic/traffic.go`:
  - `SavePayload(trafficID int64, reqBody, respBody []byte) error`
  - `GetPayload(trafficID int64) (*PayloadEntry, error)`
  - `PurgePayloads(maxAge time.Duration) (int64, error)`
- [x] **Step 3: Run test to verify pass**
- [x] **Step 4: Commit**
  `git commit -m "feat(traffic): add traffic_payloads storage and retention purge"`

### Task 9: Proxy Body Capture & Background Retention Purge

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `cmd/nineguard/main.go`
- Create: `internal/proxy/payload_capture_test.go`

- [x] **Step 1: Write failing test**
  Test proxy with `record_payloads = "all"`: verify row created in `traffic_payloads`.
  Test proxy with `record_payloads = "errors_only"`: verify row created only on 4xx/5xx status.
- [x] **Step 2: Implement capture in proxy**
  In `internal/proxy/proxy.go`:
  - Read `record_payloads` setting (`disabled`, `errors_only`, `all`).
  - Clamp bodies to 512KB.
  - Asynchronously save payload on request finish.
  In `cmd/nineguard/main.go`:
  - Launch background ticker (every 12h) calling `trafficMgr.PurgePayloads(7 * 24 * time.Hour)`.
- [x] **Step 3: Run test to verify pass**
- [x] **Step 4: Commit**
  `git commit -m "feat(proxy): capture request/response bodies and start TTL retention worker"`

### Task 10: Payload API & Traffic Explorer Inspector UI

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `web/static/js/views/traffic.js`
- Create: `internal/handler/payload_handler_test.go`

- [x] **Step 1: Write failing test**
  Test `GET /api/v1/traffic/{id}/payload` returns 200 with request/response JSON when payload exists, or 404 when absent.
- [x] **Step 2: Implement handler**
  Add `h.GetTrafficPayload` and route `GET /api/v1/traffic/{id}/payload`.
- [x] **Step 3: Implement Traffic Explorer UI**
  In `web/static/js/views/traffic.js` detail view:
  - Add tabs: **Overview**, **Prompt / Messages**, **Response Body**.
  - Syntax-highlight formatted JSON.
  - Add button: **Copy as cURL** (reconstructs curl command with endpoint, headers, and payload).
  - Add button: **Replay Request** (modal to re-send prompt to original or alternate model).
- [x] **Step 4: Run tests and commit**
  `git commit -m "feat(ui): add payload inspector tabs, cURL copy, and replay modal in traffic explorer"`

---

# Part 5: Full Regression & Integration Verification

### Task 11: End-to-End Build & Test Suite

- [x] **Step 1: Run full Go test suite**
  `CGO_ENABLED=0 go test -tags server ./...`
- [x] **Step 2: Run all JavaScript tests**
  `node --test web/jstest/*.test.mjs`
- [x] **Step 3: Compile multi-target binaries**
  `go build -o nineguard.exe ./cmd/nineguard`
- [x] **Step 4: Commit documentation & update plan**
  `git commit -m "docs(plans): complete future features phase 2 implementation plan"`
