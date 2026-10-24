# Anthropic Protocol & 9router Compatibility Report (2026-10-21)

Investigation findings for Anthropic SDK `/v1/messages` integration, cascaded NineGuard topology, and 9router v0.5.104 translation bugs.

---

## 1. Environment & Request Flow Topology

```
[Client: Pi Agent / Claude Code / SDK]
       │
       ▼ (Auth: x-api-key / Bearer)
[my-nineguard] (Docker image: ghcr.io/darktama/nineguard:1994926, v1.5.0)
       │
       ▼ (Forward: dual auth, path passthrough)
[office-nineguard] (Older build: <= v1.4.3)
       │
       ▼
[office-9router] (v0.5.104, port 20128)
       │
       ▼ (OAuth provider code: cc/)
[Anthropic Upstream API]
```

---

## 2. Issue 1: Tool Name Suffix Mutation (`_ide`, `_ide_ide`) on `/v1/messages`

### Symptom
- Client provides tools: `read`, `bash`, `edit`, `write`, `codemode`.
- Assistant tool calls returned to client: `bash_ide` (24x), `bash_ide_ide` (1x), `codemode_ide` (1x).
- Pi agent rejects execution: `Tool bash_ide not found`.
- Assistant enters retry loop attempting tool variations; user aborts session.

### Root Cause Analysis
1. **NineGuard verification:**
   - Scratch integration tests with mock upstream verified `my-nineguard` forwards `tools` and returns `tool_use` blocks verbatim.
   - Built-in plugins (`caveman`, `secretguard`) re-serialize `system` and message content; `tools` schema untouched.
2. **Upstream 9router cloaking:**
   - 9router v0.5.104 implements anti-ban tool cloaking in `open-sse/utils/claudeCloaking.js` (`CLAUDE_TOOL_SUFFIX = "_ide"`).
   - On `cc/` (Claude OAuth) upstream route, 9router renames offered tools: `bash` → `bash_ide`.
   - Claude model selects and emits `bash_ide`.
   - 9router response stream parser fails to decloak tool name back to `bash` due to SSE `toolNameMap` lookup misses (documented in 9router PR #4342 / issue #1592 / opencode issue #43049).
3. **Double-cloaking loop:**
   - Client sends subsequent turn with previous conversation history containing `bash_ide`.
   - 9router cloaking re-applies suffix: `bash_ide` → `bash_ide_ide`.

---

## 3. Issue 2: Thinking Lost (`🧠✗`) on Anthropic Models via OpenAI Endpoint (`/v1/chat/completions`)

### Symptom
- Client targets `9router/cc/claude-opus-5-5` via `POST /v1/chat/completions`.
- Request succeeds with HTTP `200 OK` (e.g., latency 2840ms, prompt tokens consumed).
- Traffic Explorer flags request with badge: `🧠✗ Thinking was requested but the upstream response shows no reasoning`.

### Root Cause Analysis
1. **Protocol translation mismatch:**
   - Client requests OpenAI-format reasoning (`reasoning_effort` or thinking payload).
   - 9router translates OpenAI request to Anthropic Messages format upstream.
   - Claude upstream delivers `thinking` content block.
   - 9router response translator fails to map Anthropic `thinking` blocks into standard OpenAI chat response structures:
     - Missing `choices[].delta.reasoning_content` (DeepSeek/OpenAI convention).
     - Missing `<thought>...</thought>` extraction into content.
2. **Gateway impact:**
   - NineGuard `StreamDiagnosticsParser("chat")` inspects SSE frames for `reasoning_content` / `reasoning_tokens`.
   - Upstream emits raw content delta without reasoning markers.
   - NineGuard records `ThinkingRequested: true`, `ThinkingDelivered: false`, triggering warning badge in Traffic Explorer.

---

## 4. Workarounds & Remediation Strategy

### A. Upstream Fixes (9router)
- Upgrade `office-9router` to build containing PR #4342 ("decloak tool names when toolNameMap misses").
- Disable Claude tool cloaking in 9router configuration for `cc/` provider if supported.

### B. Gateway Mitigation (NineGuard Planned Features)
1. **Tool Name Auto-Decloak (Option A - Core / Option B - Plugin `toolguard`):**
   - Store offered tool names from client request.
   - In response handler (both SSE and buffered JSON), compare `tool_use.name` / `function.name` against offered list.
   - If called tool ends with known cloaking suffixes (`_ide`, `_cc`) and stripped name exists in offered list, strip suffix before returning to client.
2. **Tool Mismatch Observability:**
   - Add `tools_offered` and `tools_called` columns to `traffic_logs`.
   - Flag `tool_mismatch = 1` when response tool was not in request schema.
   - Add `⚠ Tool mismatch` filter chip and detail inspector panel in Traffic Explorer.
3. **Format Visibility & Error Audits:**
   - Set `api_format` on all error rows (401, 403, 404, 429, 500).
   - Expose API Protocol filter chip (`chat` / `messages`) in Traffic Explorer.
