# 0014. Native Anthropic Messages Protocol and Dual-Header Authentication

- Status: Accepted
- Date: 2026-10-21
- Related: [0001](0001-http-plugins.md) (Plugins), [0005](0005-token-quotas-utc-windows.md) (Key Quotas), [0007](0007-model-quotas-per-key.md) (Model Quotas), [0011](0011-proxy-requests-stream-usage.md) (Streaming Usage)

## Context

NineGuard originally targeted OpenAI-compatible HTTP clients, exposing `/v1/chat/completions` and reading `Authorization: Bearer <sk-ng-...>`.

Developers using Anthropic SDKs, Claude Code, and Claude Desktop face limitations:
1. Translating Anthropic requests to OpenAI chat completions strips or degrades extended thinking blocks (`type: "thinking"`), cryptographic thinking signatures, prompt caching breakpoints (`cache_control`), and tool call block structures.
2. The Anthropic SDK authenticates via the `x-api-key` header instead of `Authorization: Bearer`.
3. Inbound requests to `/v1/messages` bypassed the plugin pipeline, leaving token savers (Caveman, Ponytail) and security filters (SecretGuard) inactive on Anthropic clients.
4. Upstream aggregators like 9router expose native Anthropic subproviders alongside OpenAI and Gemini pools.

Options considered:
1. **Full Bidirectional Translation:** Convert `/v1/messages` $\leftrightarrow$ `/v1/chat/completions` dynamically. High complexity, lossy conversion for thinking and caching.
2. **Native Passthrough with Dual-Header Auth and Schema-Aware Plugins:** Accept both `Authorization` and `x-api-key` on the same `sk-ng-...` credentials, forward `/v1/messages` natively to supporting upstreams, adapt plugin injection for Anthropic's top-level `system` field and block schema, and format errors and chat command responses according to the wire protocol.
3. **OpenAI Only with Client Wrapper:** Reject `/v1/messages` and force users through client-side translation proxies.

## Decision

Option 2.

### 1. Dual-Header Authentication
- `internal/keys.ValidateClientKey` accepts credentials from either `Authorization: Bearer <key>` or `x-api-key: <key>`.
- Client keys remain unified (`sk-ng-...`). Key quotas, model quotas, and permissions apply identically across both headers.

### 2. Native Passthrough Routing
- Requests to `POST /v1/messages` are routed and forwarded upstream as `POST /v1/messages` without protocol translation.
- Model prefix stripping and alias resolution (`ResolveModelID`) work identically across formats.
- Outbound upstream headers include both `Authorization: Bearer <upstream_key>` and `x-api-key: <upstream_key>`, with `anthropic-version: 2023-06-01` forwarded or defaulted.

### 3. Schema-Aware Plugin Pipeline
- The plugin pipeline executes for both `chat` (`/v1/chat/completions`) and `messages` (`/v1/messages`).
- **Caveman & Ponytail:** In Anthropic format, system prompts cannot be placed in the `messages` array. Injections target the top-level `system` field using polymorphic prepending (preserving text or content block arrays and `cache_control` breakpoints).
- **SecretGuard:** Recursively scans top-level `system`, message text blocks (`type: "text"`), `tool_result` content, and `tool_use.input` JSON fields. Image base64 payloads are ignored.
- **Headroom:** Gracefully skipped on `messages` format until native schema support is added.
- **HTTP Plugins:** Receive `api_format: "messages"` in the request context.

### 4. Wire-Compliant Interceptions and Errors
- Chat command interception (`/quota`, `/limits`, `/usage`) inspects user prompts on `/v1/messages` and responds with native Anthropic message payloads or SSE event streams (`message_start` $\rightarrow$ `content_block_start` $\rightarrow$ `content_block_delta` $\rightarrow$ `content_block_stop` $\rightarrow$ `message_delta` $\rightarrow$ `message_stop`).
- Client error responses on `/v1/messages` use the Anthropic envelope `{"type": "error", "error": {"type": "...", "message": "..."}}`.

## Consequences

- Anthropic SDK, Claude Code, and Claude Desktop work directly against NineGuard without translation adapters.
- Extended thinking and prompt caching operate at full fidelity through supporting upstreams (e.g. 9router Anthropic connections).
- Default token savers and safety guardrails apply automatically to Anthropic traffic.
- Zero changes to database schema or existing OpenAI-compatible agent workflows.
