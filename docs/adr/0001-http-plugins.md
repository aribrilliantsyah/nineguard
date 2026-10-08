# 0001. Third-party plugins are HTTP services

- Status: Accepted
- Date: 2026-10-06

## Context

NineGuard needs third-party plugins that transform chat completion requests (token savers, prompt injection, future security filters). NineGuard ships as a single pure-Go binary built with `CGO_ENABLED=0`, deployed mostly in Docker on small VMs.

Options considered:

1. **Go `plugin` package** — requires CGO, Linux/macOS only, plugin must be built with the exact same Go version and dependencies. Incompatible with the pure-Go build.
2. **WASM (wazero)** — pure Go and sandboxed, but plugin authors need a WASM toolchain, and passing large JSON payloads across the boundary is awkward.
3. **Subprocess (stdin/stdout or hashicorp/go-plugin)** — NineGuard must manage plugin process lifecycles and ship plugin binaries inside its container.
4. **Embedded scripting (goja, Starlark)** — simple, but slow on large payloads and limited in what plugins can do.
5. **HTTP services** — plugin runs as its own process or container; NineGuard calls it over HTTP.

## Decision

Third-party plugins are HTTP services. NineGuard sends the request plus context to the plugin's URL and receives a transformed request or a rejection. Built-in plugins are compiled Go code behind the same interface.

## Consequences

- Plugins can be written in any language and deployed as separate containers. Headroom, already an HTTP service, fits naturally.
- Every plugin call adds network latency; per-plugin timeouts and a failure policy are mandatory.
- Prompts leave the NineGuard process. Registering a plugin is an admin-only action and audited, because a malicious URL could exfiltrate prompts.
- No in-process sandboxing is needed; isolation comes from process/container boundaries.
- If in-process plugins become necessary later, WASM can be added behind the same interface without changing the HTTP contract.
