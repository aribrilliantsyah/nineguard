# Native Anthropic Messages Protocol & Schema-Aware Plugins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement native Anthropic Messages protocol support (`POST /v1/messages`), dual-header client authentication (`Authorization: Bearer` and `x-api-key`), schema-aware plugin pipeline execution (polymorphic system prompt injection for Caveman/Ponytail, deep SecretGuard scanning, Headroom graceful skip), native chat commands (`/quota`, `/usage`), and format-aware error envelopes.

**Architecture:** Inbound requests extract credentials from either `Authorization` or `x-api-key` and validate against the existing unified API key store. Routing preserves the native `/v1/messages` endpoint when forwarding upstream, passing both Bearer and Anthropic headers with `anthropic-version`. The plugin pipeline adapts to the Anthropic schema: Caveman and Ponytail target the top-level `system` field (preserving text blocks and `cache_control` breakpoints), SecretGuard deep-scans content blocks and tool payloads, and Headroom skips cleanly. Slash commands return Anthropic-compliant JSON or streaming SSE event sequences. Errors on `/v1/messages` return Anthropic error envelopes.

**Tech Stack:** Go 1.22+, SQLite (`modernc.org/sqlite`, **one open connection**), vanilla ES modules, `node --test`.

**Spec:** `docs/superpowers/specs/2026-10-21-native-anthropic-messages-design.md`, ADR 0014, `GLOSSARY.md`. Where this plan and the spec differ, **this plan wins**.

---

## Global Constraints

- Gates (`AGENTS.md` §2):
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs` (must use glob)
  4. `node --check` on every edited `.js`
- **SQLite has one open connection:** Never start a query while another `rows` is open; inside a tx use only `tx`. Read all rows before issuing writes.
- **Protocol preservation:** Do not perform cross-protocol translation between OpenAI `chat` and Anthropic `messages`. Forward requests natively to supporting upstreams.
- **Plugin safety on Anthropic:** Never inject `{"role": "system"}` into the Anthropic `messages` array. Always use the top-level `system` field.
- **Atomic commits per task:** Conventional commits (`feat(proxy): ...`, `feat(plugins): ...`). Squash into one atomic commit before merging to `main`.

---

## Spec Corrections

Any deviations found during plan execution must be recorded here and synced back to the spec in Task 8.

1. None yet.

---

### Task 1: Dual-Header Authentication and Inbound Validation

**Files:**
- Edit: `internal/proxy/proxy.go`
- Edit: `internal/proxy/proxy_test.go`

**Produces:**
- Client authentication supporting `x-api-key` header when `Authorization` is absent.
- Full compatibility of all existing and new `sk-ng-...` keys across both headers.

- [x] **Step 1: Write failing tests in `internal/proxy/proxy_test.go`**
  - Test `GET /v1/models` authenticating with `x-api-key: <key>`.
  - Test `POST /v1/chat/completions` authenticating with `x-api-key: <key>`.
  - Test `POST /v1/messages` authenticating with `x-api-key: <key>`.
  - Test empty and invalid `x-api-key` returning 401.

- [x] **Step 2: Update `internal/proxy/proxy.go` inbound auth extraction**
  Extract `clientAuth := r.Header.Get("Authorization")`; if empty, fallback to `r.Header.Get("x-api-key")`.

- [x] **Step 3: Run tests and verify pass**
  Run `go test -v ./internal/proxy -run TestDualHeaderAuth`.

---

### Task 2: Format-Aware Error Envelopes

**Files:**
- Edit: `internal/proxy/proxy.go`
- Create: `internal/proxy/error_test.go`

**Produces:**
- Centralized helper `writeProxyError(w http.ResponseWriter, r *http.Request, statusCode int, errType, errCode, errMsg string)`.
- Format-aware error envelope: Anthropic envelope `{"type":"error","error":{"type":"<type>","message":"..."}}` for `messages` format; OpenAI envelope `{"error":{"message":"...","type":"...","code":"..."}}` for `chat` and other formats.

- [x] **Step 1: Write failing tests in `internal/proxy/error_test.go`**
  - Verify 401 on `/v1/messages` produces Anthropic error envelope with `authentication_error`.
  - Verify 403 on `/v1/messages` produces Anthropic error envelope with `permission_error`.
  - Verify 429 on `/v1/messages` produces Anthropic error envelope with `rate_limit_error`.
  - Verify errors on `/v1/chat/completions` retain OpenAI error envelope.

- [x] **Step 2: Implement format-aware error writer in `internal/proxy/proxy.go`**
  Map HTTP status codes and error types to Anthropic conventions when `DetectAPIFormat(r.URL.Path) == "messages"`. Replace all inline error responses in `ServeHTTP` with this helper.

- [x] **Step 3: Run tests and verify pass**
  Run `go test -v ./internal/proxy -run TestProxyErrors`.

---

### Task 3: Upstream Headers & Messages Forwarding

**Files:**
- Edit: `internal/proxy/proxy.go`
- Edit: `internal/proxy/diagnostics_proxy_test.go`

**Produces:**
- Upstream request header population: `Authorization: Bearer <upstream_key>` and `x-api-key: <upstream_key>`.
- Protocol headers: default `anthropic-version: 2023-06-01` when absent on `/v1/messages`, and forward `anthropic-beta`.

- [x] **Step 1: Write test in `internal/proxy/diagnostics_proxy_test.go`**
  - Mock upstream verifying received headers: `Authorization`, `x-api-key`, and `anthropic-version`.
  - Verify request forwarded to `/v1/messages` intact.

- [x] **Step 2: Implement header injection in `internal/proxy/proxy.go`**
  In outbound request construction (around line 603):
  - Set both `Authorization` and `x-api-key` headers with `provider.APIKey`.
  - If `apiFormat == "messages"`, ensure `anthropic-version` is present (default `"2023-06-01"`).

- [x] **Step 3: Run tests and verify pass**
  Run `go test -v ./internal/proxy -run TestProxyAnthropicMessagesIntegration`.

---

### Task 4: Schema-Aware Built-in Plugins (Caveman & Ponytail)

**Files:**
- Edit: `internal/plugins/builtin/caveman.go`
- Edit: `internal/plugins/builtin/caveman_test.go`
- Edit: `internal/plugins/builtin/ponytail.go`
- Edit: `internal/plugins/builtin/ponytail_test.go`

**Produces:**
- `ApplyCavemanToMessages(body map[string]any, settings map[string]any) (modifiedBody map[string]any, skipped bool, skipReason string, overhead int, err error)`
- `ApplyPonytailToMessages(body map[string]any, settings map[string]any) (modifiedBody map[string]any, skipped bool, skipReason string, overhead int, err error)`
- Polymorphic prepending into top-level `system` field (nil, string, or block array).

- [x] **Step 1: Write failing tests in `internal/plugins/builtin/caveman_test.go` and `ponytail_test.go`**
  - Test injection when `system` is omitted: becomes string.
  - Test injection when `system` is string: prepended with newline delimiter.
  - Test injection when `system` is `[]any` (content blocks): text block prepended at index 0, existing blocks and `cache_control` preserved.
  - Test idempotency: skipped when marker already present in string or block array.

- [x] **Step 2: Implement polymorphic injection in `caveman.go` and `ponytail.go`**
  Add helper `injectSystemPrompt(body map[string]any, marker, prompt string) (map[string]any, bool, string, int)` shared by both plugins.

- [x] **Step 3: Run tests and verify pass**
  Run `go test -v ./internal/plugins/builtin -run "TestCaveman|TestPonytail"`.

---

### Task 5: Schema-Aware Built-in Plugins (SecretGuard & Headroom)

**Files:**
- Edit: `internal/plugins/builtin/secretguard.go`
- Edit: `internal/plugins/builtin/secretguard_test.go`
- Edit: `internal/plugins/builtin/headroom.go`
- Edit: `internal/plugins/builtin/headroom_test.go`

**Produces:**
- `ApplySecretGuardToMessages(body map[string]any, settings map[string]any) (modifiedBody map[string]any, skipped bool, rejected bool, rejectMsg string, err error)`
- Deep text inspection across `system`, text content blocks, `tool_result` content, and `tool_use.input` JSON fields.
- Headroom graceful skip on `messages` format with `skipReason = "unsupported_format"`.

- [x] **Step 1: Write failing tests in `internal/plugins/builtin/secretguard_test.go`**
  - Secret in top-level `system` string: blocked / redacted.
  - Secret inside `type: "text"` block in `messages`: blocked / redacted.
  - Secret inside `type: "tool_result"` content: blocked / redacted.
  - Secret inside `type: "tool_use"` input: blocked / redacted.
  - Image block payloads ignored.
- [x] **Step 2: Write failing test in `internal/plugins/builtin/headroom_test.go`**
  - Verify Headroom returns `skipped: true`, `skipReason: "unsupported_format"` when invoked on a messages payload.

- [x] **Step 3: Implement deep text scanner in `secretguard.go` and format guard in `headroom.go`**
  Implement recursive text scanner and redaction for Anthropic content blocks. Guard Headroom execution.

- [x] **Step 4: Run tests and verify pass**
  Run `go test -v ./internal/plugins/builtin -run "TestSecretGuard|TestHeadroom"`.

---

### Task 6: Plugin Pipeline Execution Gate & HTTP Plugin Context

**Files:**
- Edit: `internal/plugins/http.go`
- Edit: `internal/plugins/pipeline.go`
- Edit: `internal/plugins/pipeline_test.go`
- Edit: `internal/proxy/proxy.go`

**Produces:**
- `PipelineExecutor.Execute` accepting `apiFormat string`.
- Routing plugins appropriately depending on `chat` vs `messages` schema.
- Proxy pipeline gate triggering on both `chat` and `messages`.

- [x] **Step 1: Write failing test in `internal/plugins/pipeline_test.go`**
  - Execute full pipeline on a `messages` payload with Caveman and SecretGuard active.
  - Verify Caveman injected into `system`, SecretGuard inspected, Headroom skipped, and results aggregated.

- [x] **Step 2: Update `internal/plugins/http.go` and `pipeline.go`**
  - Add `APIFormat` to `HTTPPluginContext`.
  - Pass `apiFormat` to `Execute`; branch built-in execution between `ApplyCaveman` vs `ApplyCavemanToMessages`, etc.
- [x] **Step 3: Update `internal/proxy/proxy.go`**
  - Update execution condition: `isChatOrMessages := (r.Method == http.MethodPost && (apiFormat == "chat" || apiFormat == "messages"))`.
  - Pass `apiFormat` into `p.plugins.ExecutePipeline`.

- [x] **Step 4: Run tests and verify pass**
  Run `go test -v ./internal/plugins ./internal/proxy`.

---

### Task 7: Native Messages Chat Commands (`/quota`, `/usage`)

**Files:**
- Edit: `internal/proxy/chat_command.go`
- Edit: `internal/proxy/chat_command_test.go`
- Edit: `internal/proxy/proxy.go`

**Produces:**
- Slash command extraction from Anthropic messages (`messages` array with string or content blocks).
- Response generator emitting Anthropic JSON message or streaming SSE event sequences.

- [x] **Step 1: Write failing tests in `internal/proxy/chat_command_test.go`**
  - Test command extraction from Anthropic string content and block content array.
  - Test non-streaming response output shape (`type: "message"`).
  - Test streaming response output SSE events (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`).

- [x] **Step 2: Implement command extraction and response writers in `internal/proxy/chat_command.go`**
  - `ExtractMessagesChatCommand(body []byte) (cmd string, arg string, isCmd bool)`
  - `WriteAnthropicChatCommandResponse(w http.ResponseWriter, cmd, markdown string, stream bool)`

- [x] **Step 3: Wire into `internal/proxy/proxy.go`**
  Trigger chat command interception when `isChatOrMessages` and a slash command is detected.

- [x] **Step 4: Run tests and verify pass**
  Run `go test -v ./internal/proxy -run TestChatCommand`.

---

### Task 8: Documentation, Verification Gate & Spec Sync

**Files:**
- Edit: `docs/superpowers/specs/2026-10-21-native-anthropic-messages-design.md` (set Status: Implemented)
- Edit: `docs/AGENTS_SETUP.md` (add Anthropic SDK & Claude Code instructions)
- Edit: `README.md` & `README.id.md`

- [x] **Step 1: Update documentation**
  - Add Claude Code / Anthropic SDK configuration guide to `docs/AGENTS_SETUP.md`.
  - Update feature list in `README.md` and `README.id.md`.
  - Set spec status to `Implemented`.

- [x] **Step 2: Run all verification quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`

- [x] **Step 3: Commit**
  Atomic conventional commit: `feat(proxy): native anthropic messages protocol with dual-header auth and schema-aware plugins`.
