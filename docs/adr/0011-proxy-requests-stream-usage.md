# 0011. The proxy core asks upstream for streaming usage

- Status: Accepted
- Date: 2026-10-19

## Context

Plugins are the only part of NineGuard that transforms a request (GLOSSARY: Plugin). The proxy core forwards the body unchanged apart from the resolved model name.

OpenAI-compatible upstreams only send token usage on a stream when the client sets `stream_options.include_usage`. Most agents do not. When no usage arrives, the proxy guesses: completion = SSE chunk count, prompt = request bytes / 4 (`internal/proxy/proxy.go`). Quotas, reports, and the planned Estimated Cost and cache diagnostics all rest on those numbers, and the guess carries no cache or reasoning counts at all.

Options considered:

1. Leave requests alone; mark guessed rows as Estimated Usage.
2. Inject `include_usage` and pass the extra usage chunk through to the client.
3. Inject `include_usage` only when the client did not set it, and strip the extra usage-only chunk before it reaches a client that never asked for it.
4. Do it as a Built-in Plugin.

## Decision

Option 3, in the proxy core.

- On streaming `POST .../chat/completions`, if `stream_options.include_usage` is absent or false, the proxy sets it to `true` and remembers that it did.
- On the way back, a chunk with an empty `choices` array and a `usage` object is not written to a client whose request was modified. Chunks that carry content are always forwarded unchanged, even if they also carry `usage`.
- Rows that still end with no reported usage are flagged Estimated Usage (never priced, see ADR 0012).
- A global setting `stream_usage_injection` (default on) turns this off for upstreams that reject `stream_options`.

A plugin was rejected: plugins can be bound off per scope or bypassed per request, and accounting must not depend on plugin bindings. It also runs on traffic that no plugin touches.

## Consequences

- Real usage, including cache and reasoning counts, on nearly all streamed traffic. Quotas become more accurate; keys whose streams were under-counted by the guess may reach their limits sooner.
- The core now changes request bodies in one narrow, documented way. `ARCHITECTURE.md` must say so.
- Chained NineGuards compose: the child injects, the parent sees a client that asked and passes usage through, the child strips it for its own client.
- Clients see no difference unless they asked for usage themselves.
