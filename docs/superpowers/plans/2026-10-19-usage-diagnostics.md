# Usage Diagnostics Implementation Plan (Phase 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide end-to-end usage diagnostics across upstream providers and formats: real streaming usage extraction (ADR 0011), Anthropic and Responses parsing, cache read/write and reasoning tracking, thinking requested/delivered verification, and model fallback detection.

**Architecture:** The proxy core inspects inbound paths (`chat`, `messages`, `responses`), injects `stream_options.include_usage` for OpenAI streams when absent, and converts the SSE relay to event-buffered streaming so injected usage-only chunks can be parsed without reaching clients. Responses across all 3 formats are parsed into normalized token counts (`prompt`, `completion`, `total`, `cache_read`, `cache_write`, `reasoning`). Family normalization matches requested vs served models and detects silent fallback. All telemetry is recorded to `traffic_logs` with UI badges, filters, and detail inspection.

**Tech Stack:** Go 1.22+, SQLite (`modernc.org/sqlite`, **one open connection**), vanilla ES modules, `node --test`.

**Spec:** `docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md`, ADR 0011, `GLOSSARY.md`. Where this plan and the spec differ, **this plan wins** (see "Spec corrections", applied to the spec in the last task).

---

## Global Constraints

- Gates (`AGENTS.md` §2):
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs` (must use glob)
  4. `node --check` on every edited `.js`
- **SQLite has one open connection:** Never start a query while another `rows` is open; inside a tx use only `tx`. Read all rows before issuing writes.
- **Proxy changes request bodies ONLY as allowed by ADR 0011:** Inject `stream_options.include_usage = true` on `chat` streams when setting is on; never touch message content or other fields. Never re-marshal with `chatRequest` (drops unknown fields).
- **Format boundary:** Plugins and chat commands only run on `chat` (`/v1/chat/completions`). `/v1/messages` and `/v1/responses` pass through read-only with response usage parsed.
- **Atomic commits per task:** Conventional commits (`feat(traffic): ...`, `feat(proxy): ...`). Squash before merging to `main`.

---

## Spec Corrections

Any deviations found during plan execution must be recorded here and synced back to the spec in Task 8.

1. None yet.

---

### Task 1: Pure Family Normalization and Fallback Math

**Files:**
- Create: `internal/models/family.go`
- Create: `internal/models/family_test.go`

**Produces:**
- `NormalizeFamily(id string) string`
- `IsModelFallback(resolvedModelID, servedModel string) bool`

- [x] **Step 1: Write failing tests in `internal/models/family_test.go`**
  Cover spec §4.4 cases:
  - Simple IDs: `gpt-4o` -> `gpt-4o`, `claude-3-5-sonnet` -> `claude-3-5-sonnet`.
  - Date suffixes: `claude-opus-4-5-20251101` -> `claude-opus-4-5`, `gpt-4o-2024-08-06` -> `gpt-4o`, `claude-3-opus-20240229` -> `claude-3-opus`.
  - Provider prefixes (keep only after last `/`): `9router/claude-opus-4.5` -> `claude-opus-4-5`, `nineguard/9router/claude-opus-4.5` -> `claude-opus-4-5`, `openrouter/anthropic/claude-opus-4.5` -> `claude-opus-4-5`.
  - Cloud/region prefixes: `anthropic.claude-opus-4-6-v1` -> `claude-opus-4-6`, `us.anthropic.claude-3-5-sonnet` -> `claude-3-5-sonnet`, `global.anthropic.claude-opus-4-7` -> `claude-opus-4-7`.
  - Suffixes: `-latest`, `:thinking`, `-thinking`, `:free`, `:beta`, `-v1:0`, `@20250929`.
  - Normalization of dots and underscores to hyphens (`claude-opus-4.5` -> `claude-opus-4-5`).
  - Idempotence: `NormalizeFamily(NormalizeFamily(x)) == NormalizeFamily(x)`.
  - `IsModelFallback`:
    - `resolved="9router/claude-opus-4.5", served="claude-opus-4-5-20251101"` -> false (same family).
    - `resolved="9router/claude-opus-4.5", served="claude-sonnet-4-5-20250929"` -> true (fallback).
    - `served=""` -> false (no evidence).
    - `resolved="custom-model", served="custom-model"` -> false.
  Run `go test ./internal/models` -> FAIL (undefined).

- [x] **Step 2: Implement `internal/models/family.go`**
  Implement `NormalizeFamily` and `IsModelFallback` following spec §4.4.

- [x] **Step 3: Run unit tests**
  `CGO_ENABLED=0 go test -tags server ./internal/models` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/models/family.go internal/models/family_test.go`
  `git commit -m "feat(models): family normalization and fallback detection"`

---

### Task 2: Schema Migration, Settings, and Traffic Types

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/usage_diagnostics_migration_test.go`
- Modify: `internal/traffic/traffic.go`
- Modify: `internal/traffic/quota.go`
- Modify: `internal/traffic/quota_test.go`

**Produces:**
- New columns in `traffic_logs`: `api_format`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`, `thinking_requested`, `thinking_delivered`, `served_model`, `model_fallback`, `usage_estimated`.
- New setting in `settings`: `stream_usage_injection` (default `'true'`).
- Getters/setters on `*traffic.Manager`: `IsStreamUsageInjectionEnabled() bool`, `SetStreamUsageInjectionEnabled(bool) error`.
- Updated `LogEntry` struct in `internal/traffic/traffic.go` and updated `Record()` insert statement.

- [x] **Step 1: Write failing migration and settings tests**
  Create `internal/db/usage_diagnostics_migration_test.go`:
  - Verify migration adds all 9 columns idempotently on existing databases.
  - Verify `stream_usage_injection` setting defaults to `'true'`.
  In `internal/traffic/quota_test.go`:
  - Test `IsStreamUsageInjectionEnabled` and `SetStreamUsageInjectionEnabled`.
  Run `go test ./internal/db ./internal/traffic` -> FAIL.

- [x] **Step 2: Implement schema migration and settings**
  In `internal/db/db.go`:
  - Add `ALTER TABLE traffic_logs ADD COLUMN ...` statements in `migrate()`.
  - Add `INSERT OR IGNORE INTO settings (key, value) VALUES ('stream_usage_injection', 'true')`.
  In `internal/traffic/traffic.go`:
  - Extend `LogEntry` with the 9 fields (`ApiFormat string`, `CacheReadTokens int`, `CacheWriteTokens int`, `ReasoningTokens int`, `ThinkingRequested bool`, `ThinkingDelivered bool`, `ServedModel string`, `ModelFallback bool`, `UsageEstimated bool`).
  - Update `Record()` INSERT query and parameters to persist the new fields.
  - Update `Scan()` in select queries in `traffic.go`.
  In `internal/traffic/quota.go`:
  - Add `IsStreamUsageInjectionEnabled()` and `SetStreamUsageInjectionEnabled()`.

- [x] **Step 3: Run database and traffic tests**
  `CGO_ENABLED=0 go test -tags server ./internal/db ./internal/traffic` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/db/ internal/traffic/`
  `git commit -m "feat(traffic): usage diagnostics schema migration and log entry fields"`

---

### Task 3: Stream Injection and Event-by-Event SSE Relay

**Files:**
- Create: `internal/proxy/stream_inject.go`
- Create: `internal/proxy/stream_inject_test.go`
- Modify: `internal/proxy/proxy.go`
- Create: `internal/proxy/stream_relay_test.go`

**Produces:**
- Request body injection for `stream_options.include_usage = true` preserving arbitrary JSON payload fields (`map[string]json.RawMessage`).
- Event-by-event SSE buffer and relay in `proxy.go` (reading until `\n\n`), replacing the line-by-line relay.
- Event filtering: when `injectedUsage == true`, drop events that contain only `usage` and empty `choices` from client response stream while extracting usage metrics.

- [x] **Step 1: Write failing tests for stream injection**
  In `internal/proxy/stream_inject_test.go`:
  - Absent `stream_options`: injects `{"include_usage": true}`. Preserves unknown fields (`tools`, `extra_param: {"nested": 1}`).
  - `stream_options` exists with `include_usage: false`: sets `include_usage: true`.
  - `stream_options` already has `include_usage: true`: does not modify, returns `injected = false`.
  - Invalid JSON: returns original bytes and `injected = false`.
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 2: Implement `internal/proxy/stream_inject.go`**
  Implement `InjectStreamUsage(body []byte) (newBody []byte, injected bool, err error)` using `map[string]json.RawMessage`.

- [x] **Step 3: Write failing tests for SSE event relay in `internal/proxy/stream_relay_test.go`**
  - Event split across lines and flushes.
  - Usage-only chunk dropped when `injected = true`.
  - Usage-only chunk passed through when `injected = false`.
  - Content chunks that include usage are never dropped.
  - `[DONE]` event passes through.
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 4: Integrate event-by-event relay in `internal/proxy/proxy.go`**
  - Replace line-based loop (`reader.ReadBytes('\n')`) with event-based reader splitting on double newlines (`\n\n` or `\r\n\r\n`).
  - Wire `InjectStreamUsage` into `forwardBody` pipeline when `isChat && req.Stream && streamUsageInjectionEnabled`.
  - Filter usage-only SSE event when injected.

- [x] **Step 5: Run proxy tests**
  `CGO_ENABLED=0 go test -tags server ./internal/proxy` -> PASS.

- [x] **Step 6: Commit**
  `git add internal/proxy/`
  `git commit -m "feat(proxy): stream_options injection and event-buffered SSE relay"`

---

### Task 4: Multi-Format Usage, Cache, Reasoning, and Thinking Parser

**Files:**
- Create: `internal/proxy/diagnostics.go`
- Create: `internal/proxy/diagnostics_test.go`
- Modify: `internal/proxy/proxy.go`
- Create: `internal/proxy/diagnostics_proxy_test.go`

**Produces:**
- Path classifier: `DetectAPIFormat(path string) string` (`chat`, `messages`, `responses`, `other`).
- Request inspector: `DetectThinkingRequested(format string, body []byte, modelName string) bool`.
- Streaming SSE usage parser for OpenAI, Anthropic, and Responses protocols.
- Non-streaming response parser for OpenAI, Anthropic, and Responses protocols.
- Extraction of: `PromptTokens`, `CompletionTokens`, `TotalTokens`, `CacheReadTokens`, `CacheWriteTokens`, `ReasoningTokens`, `ThinkingDelivered`, `ServedModel`, `UsageEstimated`.

- [x] **Step 1: Write failing tests for format and thinking request detection**
  In `internal/proxy/diagnostics_test.go`:
  - `DetectAPIFormat`:
    - `/v1/chat/completions` -> `chat`
    - `/chat/completions` -> `chat`
    - `/v1/messages` -> `messages`
    - `/messages` -> `messages`
    - `/v1/responses` -> `responses`
    - `/responses` -> `responses`
    - `/v1/embeddings` -> `other`
  - `DetectThinkingRequested`:
    - `chat` with `reasoning_effort: "medium"` -> true
    - `chat` with `reasoning_effort: "none"` -> false
    - `chat` with `reasoning: {"enabled": true}` or `{"effort": "low"}` or `{"max_tokens": 1024}` -> true
    - `chat` with `thinking: {"type": "enabled"}` -> true
    - `messages` with `thinking: {"type": "enabled"}` -> true
    - `responses` with `reasoning: {"effort": "high"}` -> true
    - Any format with model ID `openrouter/anthropic/claude-3.7-sonnet:thinking` -> true
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 2: Write failing parser tests for all response formats**
  In `internal/proxy/diagnostics_test.go`:
  - **OpenAI chat non-stream & stream**: standard usage, `prompt_tokens_details.cached_tokens`, `completion_tokens_details.reasoning_tokens`, `model`.
  - **OpenRouter chat**: `prompt_tokens_details.cache_write_tokens`, `reasoning_content` deltas.
  - **Anthropic messages non-stream**: `message.usage.input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, `content` blocks with `type: "thinking"`, `model`.
  - **Anthropic messages SSE stream**: `message_start` (initial usage + model), `content_block_start` (`type: "thinking"`), `message_delta` (output tokens).
  - **Responses non-stream & stream**: `response.completed` with `response.usage`, reasoning tokens, model.
  - **Heuristic fallback**: missing usage on 2xx sets `UsageEstimated = true`.
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 3: Implement `internal/proxy/diagnostics.go`**
  Implement parser structs, format detection, and parse functions according to spec §4.3.

- [x] **Step 4: Connect parser in `internal/proxy/proxy.go`**
  - Classify request path with `DetectAPIFormat`.
  - Compute `thinkingRequested`.
  - In response handler (both SSE stream and non-stream), feed response data into diagnostics parser.
  - Populate all new fields on `logEntry`.

- [x] **Step 5: Write end-to-end integration test in `diagnostics_proxy_test.go`**
  Test mock upstream returning Anthropic stream and non-stream, verifying `traffic_logs` stores normalized tokens.

- [x] **Step 6: Run proxy tests**
  `CGO_ENABLED=0 go test -tags server ./internal/proxy` -> PASS.

- [x] **Step 7: Commit**
  `git add internal/proxy/`
  `git commit -m "feat(proxy): multi-format diagnostics parser for chat, messages, and responses"`

---

### Task 5: Fallback Detection and Throttled Warning Logger

**Files:**
- Create: `internal/proxy/fallback.go`
- Create: `internal/proxy/fallback_test.go`
- Modify: `internal/proxy/proxy.go`

**Produces:**
- In-memory rate-limited fallback alerter: logs `slog.Warn` with `source=traffic` at most once per 10 minutes per `(key_id, model_id, served_model)`.
- Flags `logEntry.ModelFallback = true` when `IsModelFallback(resolvedModelID, servedModel)` is true.

- [x] **Step 1: Write failing tests in `internal/proxy/fallback_test.go`**
  - Fallback detected when requested `9router/claude-opus-4.5` but served `claude-sonnet-4-5-20250929`.
  - Logger throttles duplicate alerts within 10-minute window.
  - Different keys or different models alert independently.
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 2: Implement `internal/proxy/fallback.go`**
  Implement `FallbackNotifier` with `sync.Mutex` and expiration cleanup.

- [x] **Step 3: Integrate with `internal/proxy/proxy.go`**
  Check fallback on completed requests, set `logEntry.ModelFallback`, and invoke `notifier.NotifyFallback(...)`.

- [x] **Step 4: Run proxy tests**
  `CGO_ENABLED=0 go test -tags server ./internal/proxy` -> PASS.

- [x] **Step 5: Commit**
  `git add internal/proxy/`
  `git commit -m "feat(proxy): fallback detection and throttled warning logging"`

---

### Task 6: Traffic Queries, Filters, and Handler API

**Files:**
- Modify: `internal/traffic/traffic.go`
- Modify: `internal/traffic/traffic_test.go`
- Modify: `internal/handler/handler.go`
- Modify: `internal/handler/traffic_test.go`

**Produces:**
- Extended `GET /api/v1/traffic` query parser and SQL builder supporting:
  - `api_format`: `chat`, `messages`, `responses`, `other`
  - `thinking`: `requested`, `delivered`, `lost` (`thinking_requested = 1 AND thinking_delivered = 0`)
  - `fallback`: `1` or `0`
  - `estimated`: `1` or `0`
- Response objects and JSON serialization include all 9 new diagnostics fields.

- [x] **Step 1: Write failing tests in `internal/traffic/traffic_test.go` and `internal/handler/traffic_test.go`**
  - Query traffic with `thinking=lost` returns only rows with requested=1 and delivered=0.
  - Query traffic with `fallback=1` returns only fallback rows.
  - Query traffic with `api_format=messages` returns only Anthropic messages rows.
  - Query traffic with `estimated=1` returns rows with estimated tokens.
  - Verify detail response carries all diagnostics fields.
  Run `go test ./internal/traffic ./internal/handler` -> FAIL.

- [x] **Step 2: Update SQL queries in `internal/traffic/traffic.go`**
  - Add query conditions for `api_format`, `thinking`, `fallback`, `estimated`.
  - Add columns to `SELECT` and `Scan()`.

- [x] **Step 3: Update `parseFilterParams` in `internal/handler/handler.go`**
  Parse the new query params from `r.URL.Query()` and pass to `traffic.Manager`.

- [x] **Step 4: Run traffic and handler tests**
  `CGO_ENABLED=0 go test -tags server ./internal/traffic ./internal/handler` -> PASS.

- [x] **Step 5: Commit**
  `git add internal/traffic/ internal/handler/`
  `git commit -m "feat(traffic): diagnostics filters for api_format, thinking, fallback, and estimated"`

---

### Task 7: UI — Traffic Explorer Badges, Cache Indicator, and Filters

**Files:**
- Create: `web/static/js/diagnosticshelpers.js`
- Create: `web/jstest/diagnosticshelpers.test.mjs`
- Modify: `web/static/js/views/traffic.js`

**Produces:**
- Modular UI helper `web/static/js/diagnosticshelpers.js`:
  - `formatCacheHitRate(cacheRead, prompt)` -> percentage string or `""`.
  - `thinkingBadge(thinkingReq, thinkingDel, reasoningTokens)` -> badge descriptor (`{ text, cls, title }` or `null`).
  - `fallbackBadge(modelFallback, requestedModel, servedModel)` -> badge descriptor or `null`.
- Traffic Explorer row rendering:
  - Thinking badge: `🧠 2.1k` (ok/blue), or `🧠✗` (grey/warn) for thinking lost.
  - Model Fallback badge: `⚠ fallback` with tooltip.
  - Estimated tokens badge: `≈` indicator on token cell.
  - Cache % in token column / tooltip.
- Traffic Explorer filter controls:
  - Dropdown for API Format (`All`, `chat`, `messages`, `responses`).
  - Dropdown for Thinking (`All`, `Delivered`, `Lost`, `Requested`).
  - Checkbox / toggle for `Fallback only`.
  - Checkbox / toggle for `Estimated only`.
- Detail modal:
  - API format, Served Model, full token breakdown (prompt, completion, cache read, cache write, reasoning), thinking flags.

- [x] **Step 1: Write failing JS tests in `web/jstest/diagnosticshelpers.test.mjs`**
  - Test `formatCacheHitRate`:
    - `(900, 1000)` -> `"90%"`
    - `(0, 1000)` -> `"0%"`
    - `(0, 0)` -> `""`
  - Test `thinkingBadge`:
    - Delivered with 2100 reasoning tokens -> text `"🧠 2.1k"`.
    - Delivered without reasoning token count -> text `"🧠"`.
    - Requested but not delivered -> text `"🧠✗"`, title explains thinking was lost.
    - Not requested -> returns `null`.
  - Test `fallbackBadge`:
    - Fallback true -> text `"⚠ fallback"`, tooltip shows requested and served models.
    - Fallback false -> returns `null`.
  Run `node --test web/jstest/*.mjs` -> FAIL.

- [x] **Step 2: Implement `web/static/js/diagnosticshelpers.js`**
  Implement pure helper functions and export them.

- [x] **Step 3: Run JS tests**
  `node --test web/jstest/*.mjs` -> PASS.

- [x] **Step 4: Update `web/static/js/views/traffic.js`**
  - Import diagnostics helpers.
  - Render badges in `rowEl()`.
  - Add filter inputs to toolbar.
  - Expand detail modal to show API Format, Served Model, Cache Read, Cache Write, Reasoning Tokens, and Thinking Status.

- [x] **Step 5: Verify JS syntax & quality gates**
  `node --check web/static/js/diagnosticshelpers.js`
  `node --check web/static/js/views/traffic.js`
  `node --test web/jstest/*.mjs`

- [x] **Step 6: Commit**
  `git add web/`
  `git commit -m "feat(ui): traffic diagnostics badges, cache hit rate, and filter controls"`

---

### Task 8: Architecture Documentation and Release Verification

**Files:**
- Modify: `docs/ARCHITECTURE.md` (Indonesian, keep Indonesian)
- Modify: `AGENTS.md` (ADR 0011 note in §4)

- [x] **Step 1: Update `docs/ARCHITECTURE.md`**
  Add explanation in §2 (Request Lifecycle) and §3:
  - Inbound API format detection (`chat`, `messages`, `responses`).
  - Core stream usage injection per ADR 0011 and event-based SSE relay.
  - Response token normalization (cache read/write, reasoning tokens).
  - Silent model fallback detection.

- [x] **Step 2: Update `AGENTS.md`**
  Update §4 to mention ADR 0011 core request modification.

- [x] **Step 3: Run all quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`
  4. `node --check web/static/js/views/traffic.js`
  5. `node --check web/static/js/diagnosticshelpers.js`

- [x] **Step 4: Commit**
  `git add docs/ARCHITECTURE.md AGENTS.md`
  `git commit -m "docs: architecture and contributor guidelines for usage diagnostics"`
