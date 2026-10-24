# Native Anthropic Messages Protocol & Schema-Aware Plugins — Design Spec

- Date: 2026-10-21
- Status: Implemented
- Related: `GLOSSARY.md` (Dual-Header Authentication, Messages Protocol, Chat Command Interceptor, Token Saver, Plugin Pipeline), `docs/adr/0014-native-anthropic-messages-and-dual-header-auth.md`, `docs/adr/0001-http-plugins.md`, `docs/adr/0005-token-quotas-utc-windows.md`, `docs/adr/0007-model-quotas-per-key.md`, `docs/adr/0011-proxy-requests-stream-usage.md`

---

## 1. Problem

NineGuard historically gated ingress on `Authorization: Bearer <key>` and applied plugins solely to `POST /v1/chat/completions`.

When developers connect Anthropic-native tools (Claude Code, Anthropic Python/TypeScript SDKs, Claude Desktop):
1. **Authentication Failure:** Anthropic clients transmit credentials via `x-api-key: <key>`. NineGuard rejects these requests with `401 Unauthorized` because it only inspects `Authorization`.
2. **Plugin Bypass:** The plugin pipeline (`ExecutePipeline`) only triggers when `isChat` is true (`/v1/chat/completions`). Requests sent to `/v1/messages` bypass security guardrails (SecretGuard) and token savers (Caveman, Ponytail).
3. **Protocol Collision:** Injecting OpenAI-style system messages (`{"role": "system", "content": ...}`) into an Anthropic `messages` array causes upstream Anthropic endpoints to reject requests with `400 Bad Request: roles must alternate between "user" and "assistant"`.
4. **Chat Command Blindspot:** Developers issuing `/quota` or `/usage` inside Claude Code or Anthropic SDK sessions cannot intercept commands because interceptors assume OpenAI payload structure and return OpenAI completion schemas.
5. **Error Parsing Crashes:** When NineGuard rejects an Anthropic request (401 invalid key, 403 forbidden model, 429 quota exhausted), it returns OpenAI error envelopes (`{"error": {"message": ...}}`), causing `UndefinedPropertyError` in official Anthropic client SDKs expecting `{"type": "error", "error": {"type": "...", "message": "..."}}`.

---

## 2. Scope

### In Scope
- **Dual-Header Authentication:** Accept both `Authorization: Bearer <key>` and `x-api-key: <key>` interchangeably for all existing and newly issued `sk-ng-...` API keys.
- **Native Messages Routing:** Forward `POST /v1/messages` requests directly to supporting upstreams without lossy wire translation.
- **Upstream Auth & Protocol Headers:** Forward upstream master API keys via both `Authorization: Bearer` and `x-api-key`, and pass/default `anthropic-version: 2023-06-01`.
- **Schema-Aware Plugin Pipeline:**
  - Execute plugin pipeline on `POST /v1/messages`.
  - **Caveman & Ponytail:** Polymorphically prepend instructions into top-level `system` field (preserving text blocks and `cache_control` breakpoints).
  - **SecretGuard:** Deep text scanning across `system`, text content blocks, `tool_result` contents, and `tool_use.input` JSON fields.
  - **Headroom:** Skip gracefully on `messages` format with audit trail (`headroom:unsupported_format`).
  - **HTTP Plugins:** Propagate `api_format: "messages"` in plugin execution context.
- **Messages Chat Commands:** Intercept `/quota` and `/usage` on `/v1/messages`, returning Anthropic-compliant JSON or SSE streaming event sequences.
- **Format-Aware Error Envelopes:** Return Anthropic-compliant error JSON when requests target `/v1/messages`.

### Out of Scope
- Dynamic wire conversion between OpenAI `chat/completions` and Anthropic `messages` (e.g. proxying OpenAI clients to direct Anthropic upstreams or vice-versa).
- Translating streaming OpenAI deltas to Anthropic SSE events.
- Headroom compression for Anthropic multi-block format (deferred until Headroom supports Anthropic schema).
- Changes to SQLite database schema (existing `api_keys` and `traffic_logs` schemas fully support this).

---

## 3. Inbound Authentication & Routing

### 3.1 Dual-Header Extraction
In `internal/proxy/proxy.go`, resolve the client token from either standard header:

```go
clientAuth := r.Header.Get("Authorization")
if clientAuth == "" {
    clientAuth = r.Header.Get("x-api-key")
}
```

The token is validated via `keys.ValidateClientKey(clientAuth)`. Because `ValidateClientKey` strips optional leading `Bearer ` prefixes and trims whitespace, raw tokens from `x-api-key` validate seamlessly against the existing key cache and database.

### 3.2 Upstream Forwarding Headers
When dispatching outbound requests (`outReq`) to an upstream provider:
1. Always populate standard Bearer header: `outReq.Header.Set("Authorization", "Bearer " + provider.APIKey)`.
2. Also populate Anthropic key header: `outReq.Header.Set("x-api-key", provider.APIKey)`.
3. If `apiFormat == "messages"`:
   - If incoming request has `anthropic-version`, forward it.
   - If absent, default `outReq.Header.Set("anthropic-version", "2023-06-01")`.
   - Forward `anthropic-beta` headers if present (essential for prompt caching and extended output limits).

---

## 4. Schema-Aware Plugin Pipeline

### 4.1 Pipeline Execution Gate
Expand the execution gate in `internal/proxy/proxy.go`:

```go
apiFormat := DetectAPIFormat(r.URL.Path)
isSupportedMethod := (r.Method == http.MethodPost)
isChatOrMessages := isSupportedMethod && (apiFormat == "chat" || apiFormat == "messages")

if isChatOrMessages && p.plugins != nil {
    // Run pipeline with apiFormat passed in context
}
```

### 4.2 Polymorphic System Prompt Prepending (Caveman & Ponytail)
Anthropic prohibits `{"role": "system"}` within the `messages` array. Injections must target the top-level `system` field:

1. **Omitted / Null:** Set `"system": marker + "\n" + prompt`.
2. **String:** Set `"system": marker + "\n" + prompt + "\n\n" + existingString`.
3. **Array of Content Blocks (`[]any`):**
   Prepend a new text block at index 0:
   ```json
   {
     "type": "text",
     "text": "[nineguard:caveman]\n..."
   }
   ```
   Leave subsequent blocks (and their `cache_control` annotations) intact.
4. **Idempotency:** If `marker` (`[nineguard:caveman]` or `[nineguard:ponytail]`) is detected anywhere in `system` (string or block array), skip injection with reason `"marker_present"`.

### 4.3 Deep Text Inspection (SecretGuard)
When `apiFormat == "messages"`, SecretGuard scans and redacts/blocks secrets across all textual surfaces:
1. Top-level `system` (string or text blocks).
2. For each message in `messages`:
   - If `content` is a string: scan string.
   - If `content` is a slice of blocks (`[]any`):
     - `block.type == "text"`: scan `block.text`.
     - `block.type == "tool_result"`: scan `block.content` (string or nested blocks).
     - `block.type == "tool_use"`: serialize `block.input` to JSON and scan; redact matches within serialized values if action is `redact`.
     - `block.type == "image"`: skip binary/base64 payload.

### 4.4 Graceful Skip (Headroom)
Headroom assumes OpenAI message dictionaries (`role`, `content`). When `apiFormat == "messages"`, Headroom marks the plugin as skipped (`skipped = true`, `skipReason = "unsupported_format"`), allowing the rest of the pipeline to proceed without corruption.

### 4.5 HTTP Plugins Context
Extend `HTTPPluginContext` in `internal/plugins/http.go`:
```go
type HTTPPluginContext struct {
    APIFormat  string         `json:"api_format"` // "chat" | "messages"
    APIKeyName string         `json:"api_key_name"`
    Model      string         `json:"model"`
    Provider   string         `json:"provider"`
    Groups     []string       `json:"groups"`
    Settings   map[string]any `json:"settings"`
}
```

---

## 5. Chat Commands on Native Messages (`/quota`, `/usage`)

### 5.1 Slash Command Detection on `/v1/messages`
Extract command from user prompt:
1. Inspect last message in `messages` where `role == "user"`.
2. If `content` is string: evaluate `IsChatCommand`.
3. If `content` is `[]any`: find first block with `type: "text"`, evaluate `IsChatCommand(block.text)`.

### 5.2 Non-Streaming Response
Return HTTP 200 with Anthropic message payload:
```json
{
  "id": "msg_cmd_nineguard",
  "type": "message",
  "role": "assistant",
  "model": "nineguard/system",
  "content": [
    {
      "type": "text",
      "text": "# Quota Report\n..."
    }
  ],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0
  }
}
```

### 5.3 Streaming SSE Sequence
When `stream: true`, emit a valid Anthropic SSE sequence:
```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_cmd_nineguard","type":"message","role":"assistant","model":"nineguard/system","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"# Quota Report\n..."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}

event: message_stop
data: {"type":"message_stop"}
```

---

## 6. Format-Aware Error Envelopes

Centralize proxy error generation in `internal/proxy/proxy.go`. When an error occurs (authentication failure, forbidden model, quota exceeded, plugin rejection):

If `apiFormat == "messages"`:
```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "Invalid or inactive NineGuard API key."
  }
}
```
Standard Anthropic error types mapped:
- 401 $\rightarrow$ `authentication_error`
- 403 $\rightarrow$ `permission_error`
- 404 $\rightarrow$ `not_found_error`
- 429 $\rightarrow$ `rate_limit_error`
- 400 $\rightarrow$ `invalid_request_error`
- 502/503 $\rightarrow$ `api_error`

If `apiFormat == "chat"` or other:
Retain existing OpenAI error envelope:
```json
{
  "error": {
    "message": "...",
    "type": "...",
    "code": "..."
  }
}
```

---

## 7. Diagnostics & Traffic Logging Integration

No schema additions required. `internal/proxy/diagnostics.go` already parses:
- `api_format: "messages"`
- Input tokens, cache read tokens (`cache_read_input_tokens`), cache write tokens (`cache_creation_input_tokens`), and output tokens (`output_tokens`).
- Thinking blocks (`type: "thinking"`) and thinking flags.
- Served model fallback detection.

Cost estimation engine automatically prices Anthropic traffic using `NormalizeFamily` and existing pricing snapshots.

---

## 8. Test Plan & Acceptance Criteria

1. **Authentication Tests:**
   - Verify `x-api-key: sk-ng-...` authenticates on `/v1/messages`, `/v1/chat/completions`, and `/v1/models`.
   - Verify missing/invalid `x-api-key` returns 401 with Anthropic error envelope on `/v1/messages`.
2. **Plugin Tests on `/v1/messages`:**
   - Caveman/Ponytail inject into string `system`.
   - Caveman/Ponytail inject into array `system` without dropping existing blocks or `cache_control`.
   - Caveman/Ponytail skip if marker present.
   - SecretGuard blocks/redacts API keys inside `tool_result` and `system`.
   - Headroom skips cleanly without error on `/v1/messages`.
3. **Chat Command Tests:**
   - Test non-streaming `/quota` and `/usage` on `/v1/messages` returning Anthropic payload.
   - Test streaming `/quota` on `/v1/messages` returning valid Anthropic SSE events.
4. **End-to-End Proxy Tests:**
   - Proxy `/v1/messages` to mock upstream (simulating 9router Anthropic subprovider).
   - Verify forwarded headers include `Authorization: Bearer`, `x-api-key`, and `anthropic-version`.
   - Verify model alias replacement works on `/v1/messages` (e.g. `9dak/cc/claude-opus-5` $\rightarrow$ `cc/claude-opus-5`).
   - Verify traffic log entry captures `api_format: "messages"` and accurate token counts.
