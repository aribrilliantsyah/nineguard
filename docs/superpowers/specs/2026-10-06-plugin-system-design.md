# Plugin System — Design Spec

- Date: 2026-10-06
- Status: Draft, awaiting review
- Revision 1 (2026-10-06): plugin categories, overlap warnings, upstream token-saving flag on providers, idempotency (prompt marker + applied header), built-in guidance text, worked example (§4.8, §4.9, §5.5, §9.1).
- Revision 2 (2026-10-06): scope labelling and "decided by" explanations in the UI (§9.2).
- Related: `GLOSSARY.md`, `docs/adr/0001-http-plugins.md`, `docs/adr/0002-model-group-dual-role.md`

## 1. Goal

Let NineGuard transform chat completion requests through **plugins** — token savers by default (Caveman, Ponytail, Headroom), plus third-party plugins — and control which plugins apply per **API Key**, per **Model Group**, or globally.

Unlike 9router, where token savers are global, NineGuard scopes plugins so different agents and model families get different behaviour.

## 2. Scope

### In scope (v1)

- Plugin engine inside the proxy request path.
- Built-in plugins: **Caveman**, **Ponytail**, **Headroom connector**.
- Third-party **HTTP Plugins** registered by URL.
- **Plugin Bindings** at Global, Model Group, and API Key scope with precedence.
- Global **Plugin Pipeline** order.
- Failure policy, rejection, per-request bypass header.
- **Plugin Categories** and **Overlap Warnings** (warn, never block) for stacked token savers.
- **Upstream Token Saving** flag on providers.
- Idempotency: built-ins never apply twice when the request already went through another NineGuard.
- Telemetry: plugins applied, tokens saved, plugin overhead, plugin errors, plugin latency.
- Dashboard UI and REST API for all of the above.
- RBAC and audit logging.

### Out of scope (v1)

- Response transformation (including SSE).
- Endpoints other than `POST /v1/chat/completions`. Other `/v1/*` requests pass through without plugins.
- RTK (tool-output compression filters) — planned v2, possibly as an HTTP Plugin.
- Automatic `X-9Router-Token-Saver: off` injection. Users disable 9router's RTK manually.
- Encryption of stored secrets (tracked separately, see §13).
- Token estimation via tokenizer.
- Automatic detection of token saving performed by upstream providers (not observable; users declare it via the provider flag).
- Hard mutual exclusion between plugins. Overlaps produce warnings only.
- Detecting client-side prompts (e.g. a Caveman skill installed in the agent) that carry no NineGuard marker.

## 3. Request Flow

Current flow in `internal/proxy/proxy.go`, with the new step inserted:

1. Authenticate API Key (401).
2. Read and buffer request body.
3. Per-key model access check (403 `model_not_allowed`).
4. Global model firewall (403 `model_disabled`).
5. Resolve provider by prefix (502 `no_provider`).
6. **NEW — Plugin Pipeline** (only for `POST /v1/chat/completions`):
   1. Resolve effective plugin set for (API Key, model).
   2. Drop bypassable plugins if `X-NineGuard-Plugins: off` is present.
   3. Drop bypassable built-ins already listed in an incoming `X-NineGuard-Plugins-Applied` header (§4.9).
   4. Run plugins in global pipeline order on the request body. Caveman/Ponytail additionally skip themselves when their marker is already present (§4.9).
   5. Set `X-NineGuard-Plugins-Applied` on the upstream request (§4.9).
   6. On rejection → 403 `plugin_rejected`. On fail-closed error → 503 `plugin_unavailable`. On fail-open error → continue with the body from before that plugin.
7. Rewrite model ID (strip prefix) and forward upstream.
8. Stream/relay response, record traffic (now including plugin telemetry).

Plugins see the request **before** prefix stripping (model as the client sent it), so plugin context matches what the user configured.

The `X-NineGuard-Plugins` header is removed before forwarding upstream.

## 4. Plugin Model

### 4.1 Plugin interface (Go)

```go
type Plugin interface {
    ID() string
    Transform(ctx context.Context, req *ChatRequest, pc PluginContext) (Result, error)
}

type PluginContext struct {
    APIKeyName string
    Model      string   // as sent by client
    Provider   string   // provider ID
    Groups     []string // names of Model Groups the model belongs to
    Settings   map[string]any // effective merged settings
}

type Result struct {
    Request        *ChatRequest // nil when rejected
    Rejected       bool
    RejectMessage  string
    TokensSaved    int // reported by plugin; 0 if unknown
    TokensOverhead int // tokens added (injection plugins)
}
```

`ChatRequest` is the decoded JSON body as `map[string]any` with typed accessors for `messages`, so unknown fields are preserved verbatim.

### 4.2 Plugin record

Each registered plugin (built-in or HTTP) has:

| Field | Meaning |
|---|---|
| `id` | Stable ID. Built-ins: `caveman`, `ponytail`, `headroom`. HTTP: generated. |
| `kind` | `builtin` or `http` |
| `category` | `input_compression`, `output_style`, or `other` (§4.8). Built-ins fixed; HTTP plugins choose on registration, default `other`. |
| `name`, `description` | Display |
| `url` | HTTP only (also used by Headroom connector) |
| `secret` | HTTP only, generated on registration |
| `timeout_ms` | Default 3000 (HTTP), 8000 (Headroom), n/a for injection built-ins |
| `failure_policy` | `open` or `closed`. Default `open`. |
| `bypassable` | Bool. Forced `false` when `failure_policy = closed`. |
| `pipeline_order` | Integer, global order (ascending) |
| `default_settings` | JSON |

### 4.3 Built-in: Caveman

- Prepends a `system` message containing the Caveman prompt.
- Settings: `variant` (`caveman`, `ultracave`, `megacave`; default `caveman`), `prompt_override` (string; empty = bundled default for the chosen variant).
- Variants map 1:1 to upstream skills `skills/caveman`, `skills/ultracave`, `skills/megacave` (≈4.0 KB, 2.3 KB, 2.6 KB). `megacave` replies in classical Chinese; UI labels it as such.
- Bundled prompt text from upstream Caveman (Apache-2.0) with attribution in `NOTICE`.
- Reports `TokensOverhead` = `ceil(len(injected_text) / 4)`, labelled "overhead (est.)" in UI. This is the only estimated number; Tokens Saved is never estimated.

### 4.4 Built-in: Ponytail

- Same mechanism as Caveman.
- Settings: `level` (`lite`, `full`, `ultra`; default `full`), `prompt_override`.
- Bundled text from Ponytail `AGENTS.md` (MIT) with attribution in `NOTICE`.

### 4.5 Injection position

- New `system` message **prepended at index 0**. Client's own system message is untouched.
- Text is deterministic for a given settings combination so the provider prefix cache stays stable.
- When both Caveman and Ponytail apply, each prepends in pipeline order; final order is deterministic. This combination is allowed but triggers the `output_style_overlap` warning (§4.8).
- Each injected message starts with a marker line (§4.9). The marker is part of the deterministic text, so prefix caching is unaffected.

### 4.6 Built-in: Headroom connector

- Calls `POST {url}/v1/compress` with `{messages, model, config}`.
- `system` and `tools` from the original request are preserved by NineGuard (Headroom ignores them).
- Settings:
  - `url` (default `http://127.0.0.1:8787`)
  - `mode` — `incremental` (default) or `full`
    - `incremental`: `config.frozen_message_count` = index after the last `assistant` message; only newer messages are compressed. Preserves provider prefix cache.
    - `full`: compress entire history every turn.
  - `compress_user_messages` (default `false`)
  - `token` — optional `HEADROOM_PROXY_TOKEN`
- `TokensSaved` = Headroom's `tokens_saved`.
- If Headroom returns `compression_skipped: true`, treat as success with 0 saved.
- Requires `HEADROOM_COMPRESS_ALLOW_REMOTE=1` on the Headroom side when it runs in a separate container (see §12).

### 4.7 HTTP Plugin contract

Request from NineGuard:

```http
POST {url}
Content-Type: application/json
X-NineGuard-Plugin-Secret: {secret}

{
  "request": { ...chat completion body... },
  "context": {
    "api_key_name": "Cursor IDE",
    "model": "9router/claude-sonnet",
    "provider": "9router",
    "groups": ["Claude"],
    "settings": { ... }
  }
}
```

Client IP is **not** sent.

Responses accepted:

```json
{ "request": { ...modified body... }, "tokens_saved": 120, "tokens_overhead": 0 }
```

```json
{ "action": "reject", "message": "PII detected in prompt" }
```

`tokens_saved` and `tokens_overhead` are optional. Any non-2xx status, invalid JSON, missing `request`, or timeout is a plugin error and goes through the failure policy.

The plugin must not change `model`. If it does, NineGuard restores the original value and logs a warning.

### 4.8 Plugin Categories and guidance

Every plugin has a **category** describing which part of the exchange it reduces:

| Category | Acts on | Built-ins | Risk when stacked |
|---|---|---|---|
| `input_compression` | Conversation history / tool output sent to the model | Headroom | Context the model needed is removed; the model becomes forgetful, re-asks, or makes mistakes. |
| `output_style` | How the model writes its answer (instruction prompt) | Caveman, Ponytail | Overlapping or conflicting style rules; answers become too terse and reasoning quality drops. |
| `other` | Anything else (PII filters, policy checks, …) | — | No overlap warnings. |

`input_compression` + `output_style` together is **not** an overlap (they act on different sides) and produces no warning.

Built-ins ship fixed guidance text (Go constants, not stored in DB), returned by `GET /api/v1/plugins` as `guidance`:

| Plugin | `summary` | `recommended_for` | `not_recommended_for` |
|---|---|---|---|
| Headroom | Compresses older messages and tool outputs before they are sent. Reduces input tokens; does not change answer style. | Long agent sessions, large tool outputs, expensive models. | Short chats (little to compress); providers that already compress input. |
| Caveman | Instructs the model to answer tersely, dropping filler. Reduces output tokens. | Coding agents, CLI tools. | Roleplay, creative writing, teaching/explanations; combining with Ponytail. |
| Ponytail | Instructs the model to answer compactly at a chosen level (lite/full/ultra). Reduces output tokens. | Coding agents wanting a milder style than Caveman (`lite`). | Roleplay, creative writing; combining with Caveman. |

HTTP plugins may supply `summary` at registration (optional free text); `recommended_for` / `not_recommended_for` are empty for them.

#### Overlap Warnings

Warnings are computed server-side for a resolved (API Key, model) pair. They never block a request or a save.

| Code | Condition | Message (template) |
|---|---|---|
| `output_style_overlap` | ≥ 2 effective plugins with category `output_style` | "{A} and {B} both change answer style. Stacking them can make answers too terse and lower quality. Enable only one." |
| `upstream_token_saving` | ≥ 1 effective plugin with category `input_compression` or `output_style`, and the request's provider has `upstream_token_saving = 1` | "Provider {P} is marked as already applying token saving ({note}). {plugins} may compress twice and remove context the model needs." |

The Go function is pure: `Warnings(effective []ResolvedPlugin, provider ProviderInfo) []Warning`, with `Warning{Code, Plugins []string, Provider string, Message string}`.

### 4.9 Idempotency

Prevents a built-in from applying twice when requests pass through several NineGuard instances (e.g. a provider that is itself a NineGuard).

**Prompt marker (Caveman, Ponytail).**
- The injected system message's first line is exactly `[nineguard:caveman]` or `[nineguard:ponytail]`.
- Before injecting, the plugin scans every message with role `system` or `developer` (string content, or text parts of array content). If its own marker is present, it returns the request unchanged with `Skipped = true` (reason `marker_present`), overhead 0.
- Detects only NineGuard-injected prompts. Client-side prompts without the marker are not detected (out of scope).

**Applied header (all built-ins).**
- After the pipeline, NineGuard sets `X-NineGuard-Plugins-Applied` on the upstream request: comma-separated, sorted, de-duplicated union of the incoming header value (built-in IDs only) and the built-in IDs that ran successfully in this instance. Omitted when empty.
- On an incoming request carrying the header, a built-in listed in it is skipped (reason `already_applied`) **only if that plugin is bypassable**. Non-bypassable plugins always run, since any client can send this header.
- Only built-in IDs (`headroom`, `caveman`, `ponytail`) are read from or written to the header. HTTP plugin IDs are instance-local and ignored.
- The incoming header is not forwarded as-is; the recomputed union replaces it.

`Result` gains `Skipped bool` and `SkipReason string`. Skipped plugins are recorded in telemetry (§10) but not in `plugins_applied`.

## 5. Bindings and Resolution

### 5.1 Binding record

| Field | Meaning |
|---|---|
| `plugin_id` | |
| `scope_type` | `global`, `group`, `key` |
| `scope_id` | empty for global, group ID, or API key ID |
| `state` | `on`, `off`, `inherit` |
| `settings` | JSON override, merged shallowly over broader settings |

One binding per (plugin, scope_type, scope_id). Global bindings exist for every plugin; default state `off` (opt-in).

### 5.2 Resolution per plugin

For a request with API Key K and model M:

1. Start with the Global binding (state, settings).
2. Find every Model Group whose members match M (same pattern rules as access control: exact, `*`, `prefix/*`, `*.suffix`, flexible provider prefix). Among those with a non-`inherit` binding for this plugin, take the one with the highest **Group Priority**; ties broken by group name ascending. Apply its state and merge its settings.
3. If K has a non-`inherit` binding, apply its state and merge its settings.
4. Plugin runs if final state is `on`.

Settings merge order: plugin `default_settings` ← global ← group ← key.

### 5.3 Group Priority

- New integer column on `model_groups`, default `0`.
- UI warns when two groups with equal priority have conflicting bindings for the same plugin and share at least one model pattern overlap (best-effort detection: same pattern or one is a wildcard covering the other).

### 5.4 Caching

Plugins, bindings, and group membership are loaded into memory (same pattern as `keys.Manager.ReloadGroupCache`) and reloaded on any write. Resolution must not touch the database per request.

### 5.5 Worked example (reference scenario, also a test fixture)

Providers: `9r/` (9router, `upstream_token_saving = 0` after RTK is turned off) and `office/` (another NineGuard).

| Group | Models | Linked keys | Role |
|---|---|---|---|
| Coding | `office/claude-sonnet-4`, `office/gpt-5`, `9r/claude-opus-4`, `9r/qwen3-coder` | pi-dev | access |
| Roleplay | `9r/claude-opus-4`, `9r/deepseek-v3` | marinara | access |
| Demanding | `office/claude-sonnet-4`, `9r/claude-opus-4` | none | plugins only |

| Plugin | Global | Coding | Roleplay | Demanding | Key pi-dev | Key marinara |
|---|---|---|---|---|---|---|
| Headroom | off | inherit | inherit | on | inherit | inherit |
| Caveman | off | inherit | inherit | inherit | on | inherit |
| Ponytail | off | inherit | inherit | inherit | inherit | inherit |

Expected effective sets:

| Key → Model | Runs |
|---|---|
| pi-dev → `office/claude-sonnet-4` | Headroom, Caveman |
| pi-dev → `9r/claude-opus-4` | Headroom, Caveman |
| pi-dev → `office/gpt-5` | Caveman |
| pi-dev → `9r/qwen3-coder` | Caveman |
| marinara → `9r/claude-opus-4` | Headroom |
| marinara → `9r/deepseek-v3` | none |
| marinara → `office/gpt-5` | 403 `model_not_allowed` before plugins |

Variations used as tests:
- Key marinara Headroom = `off` → marinara → `9r/claude-opus-4` runs nothing (key overrides group).
- Key pi-dev Ponytail = `on` → `output_style_overlap` warning for every pi-dev pair.
- Provider `office` flagged `upstream_token_saving = 1` → `upstream_token_saving` warning for pi-dev → `office/*` pairs; no warning for marinara → `9r/deepseek-v3` (nothing runs).

Guidance for users (README + UI help): bind behaviour that belongs to an **agent** on the API Key; bind behaviour that belongs to a **model** on a plugin-only Model Group linked to no key; leave access-control groups at `inherit`.

## 6. Pipeline Execution

- Applicable plugins run sequentially in ascending `pipeline_order`.
- Each plugin receives the output of the previous one.
- Per-plugin timeout via `context.WithTimeout`.
- Total pipeline has no extra timeout beyond the sum of plugin timeouts.
- Recommended default order: Headroom (compress) → Ponytail → Caveman, so compression never processes injected prompts.

### 6.1 Failure handling

| Event | Fail-open plugin | Fail-closed plugin |
|---|---|---|
| Timeout / network error / bad response | Skip plugin, keep previous body, record error | 503 `plugin_unavailable` |
| Explicit reject | 403 `plugin_rejected` | 403 `plugin_rejected` |

Error bodies use OpenAI format:

```json
{ "error": { "message": "...", "type": "permission_error", "param": null, "code": "plugin_rejected" } }
```

```json
{ "error": { "message": "Plugin 'pii-filter' is unavailable.", "type": "server_error", "param": null, "code": "plugin_unavailable" } }
```

Both are recorded in `traffic_logs`.

### 6.2 Bypass

- Header `X-NineGuard-Plugins: off` skips all plugins with `bypassable = true` for that request.
- Non-bypassable plugins still run.
- Header is stripped before forwarding.

## 7. Data Model

New tables:

```sql
CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,              -- builtin | http
  name TEXT NOT NULL,
  description TEXT DEFAULT '',
  url TEXT DEFAULT '',
  secret TEXT DEFAULT '',
  timeout_ms INTEGER DEFAULT 3000,
  failure_policy TEXT DEFAULT 'open',
  bypassable INTEGER DEFAULT 1,
  pipeline_order INTEGER DEFAULT 100,
  default_settings TEXT DEFAULT '{}',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS plugin_bindings (
  plugin_id TEXT NOT NULL,
  scope_type TEXT NOT NULL,        -- global | group | key
  scope_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'inherit',
  settings TEXT DEFAULT '{}',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plugin_id, scope_type, scope_id)
);
```

`plugins` also has `category TEXT NOT NULL DEFAULT 'other'` and `summary TEXT DEFAULT ''` (HTTP plugins only; built-ins use Go constants).

Column additions (via existing `ALTER TABLE ... ADD COLUMN` migration style in `internal/db/db.go`):

```sql
ALTER TABLE model_groups ADD COLUMN priority INTEGER DEFAULT 0;
ALTER TABLE providers ADD COLUMN upstream_token_saving INTEGER DEFAULT 0;
ALTER TABLE providers ADD COLUMN upstream_token_saving_note TEXT DEFAULT '';
ALTER TABLE traffic_logs ADD COLUMN plugins_skipped TEXT DEFAULT '';   -- comma-separated id:reason
ALTER TABLE traffic_logs ADD COLUMN plugins_applied TEXT DEFAULT '';   -- comma-separated IDs
ALTER TABLE traffic_logs ADD COLUMN tokens_saved INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN tokens_overhead INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN plugin_errors TEXT DEFAULT '';     -- comma-separated IDs
ALTER TABLE traffic_logs ADD COLUMN plugin_ms INTEGER DEFAULT 0;
```

Seed on startup (idempotent): insert built-ins `headroom` (order 10, `input_compression`), `ponytail` (order 20, `output_style`), `caveman` (order 30, `output_style`) and their Global bindings with state `off`. Seeding also corrects `category` on existing built-in rows.

Cascade rules:
- Deleting an API Key deletes its bindings.
- Deleting a Model Group deletes its bindings (group deletion is already blocked while linked to keys; plugin bindings do not block deletion).
- Built-in plugins cannot be deleted.

## 8. REST API

All under existing session auth.

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/v1/plugins` | any | List plugins (secret masked) with global binding |
| POST | `/api/v1/plugins` | admin | Register HTTP plugin (incl. optional `category`, `summary`); returns secret once |
| PUT | `/api/v1/plugins/{id}` | admin for `url`/`secret`/`failure_policy`; operator for the rest | Update plugin |
| POST | `/api/v1/plugins/{id}/rotate-secret` | admin | New secret, returned once |
| DELETE | `/api/v1/plugins/{id}` | admin | Delete HTTP plugin (built-ins rejected) |
| POST | `/api/v1/plugins/{id}/test` | any | Send dummy request, return latency + result |
| PUT | `/api/v1/plugins/order` | any | Body: ordered list of IDs |
| GET | `/api/v1/plugins/{id}/bindings` | any | All bindings for plugin |
| PUT | `/api/v1/plugins/{id}/bindings` | any | Upsert one binding `{scope_type, scope_id, state, settings}` |
| GET | `/api/v1/plugins/resolve?key_id=&model=` | any | Preview effective plugins and settings for a key + model, plus `warnings` |
| GET | `/api/v1/plugins/warnings` | any | All current Overlap Warnings across scopes (see §9.1) |
| POST | `/api/v1/plugins/{id}/reset-prompt` | any | Clear `prompt_override` (Caveman/Ponytail) |

`PUT /api/v1/model-groups/{id}` accepts optional `priority`.

`PUT /api/v1/providers/{id}` and `POST /api/v1/providers` accept optional `upstream_token_saving` (bool) and `upstream_token_saving_note` (string, max 200 chars); provider list returns both.

`PUT /api/v1/plugins/{id}/bindings` responds with the saved binding plus `warnings`: the Overlap Warnings this scope now produces (same shape as §9.1 entries, filtered to the scope).

Admin checks follow the existing pattern `user.Role != "admin"` in `internal/handler/handler.go`. Every write emits an audit log entry (`plugin.create`, `plugin.update`, `plugin.delete`, `plugin.rotate_secret`, `plugin.binding.update`, `plugin.order.update`).

## 9. Dashboard UI

New page **Gateway → Plugins** (`web/static/js/views/plugins.js`):

- Banner on first view after upgrade: "Plugins available — all off by default."
- Plugin list with drag-to-reorder (pipeline order), kind badge, failure policy, bypassable, global state toggle, Test button with latency.
- Plugin detail drawer:
  - Settings form (built-ins: typed fields; HTTP: JSON editor).
  - Prompt editor with "Reset to default" (Caveman/Ponytail).
  - Bindings table: Global row, one row per Model Group, one row per API Key, each with `on/off/inherit` and settings override.
  - Conflict warning for equal-priority groups.
- "Register HTTP Plugin" modal (admin only): name, URL, timeout, failure policy, bypassable. Shows generated secret once with Copy button.
- **Resolve preview**: pick API Key + model → shows which plugins run and with what settings, plus warning badges.
- Each built-in card shows its guidance (§4.8): summary, "Recommended for", "Not recommended for".

Changes to existing pages:

- **Model Groups**: priority field.
- **Providers**: checkbox "Upstream already applies token saving" with note field and help text: "Tick this if the provider does its own token saving (for example 9router RTK or another NineGuard with plugins on). NineGuard will warn when its own token savers stack on top." Badge on flagged providers in the list.
- **Endpoints & Keys**: per-key "Plugins" section linking to bindings.
- **Traffic Explorer**: columns for plugins applied, tokens saved, overhead, plugin errors, plugin ms.
- **Dashboard**: "Tokens Saved" KPI card.
- **Usage Reports**: tokens saved per key and per plugin.

### 9.1 Warnings in the UI

All warnings are advisory. Three surfaces:

1. **Confirm on enable.** Switching a binding of an `input_compression` or `output_style` plugin to `on` (from `off`/`inherit`) opens a dialog with the plugin's guidance and a checklist: "Check that your upstream providers do not already apply token saving (e.g. 9router RTK, another NineGuard). Stacked token savers can remove context and lower answer quality." Buttons: Cancel / Enable. Checkbox "Don't show again for this plugin" stored in `localStorage` (`ng.plugins.ack.{plugin_id}`).
2. **After save.** Warnings returned by the binding upsert are shown as a toast plus inline list under the bindings table.
3. **Persistent indicators.** The Plugins page calls `GET /api/v1/plugins/warnings`; a summary banner ("N warnings") and a ⚠️ icon on affected binding rows link to details.

`GET /api/v1/plugins/warnings` evaluation (in memory, no per-request cost):
- Iterate active API Keys × enabled models in the `models` table that the key may access (same ACL as the proxy).
- Resolve each pair, compute `Warnings`, and group by (key, code, plugin set, provider).
- Response entries: `{code, message, plugins[], provider, key_id, key_name, model_count, models_sample[≤5]}`.
- Hard cap of 50,000 evaluated pairs; beyond that, return `truncated: true`.

### 9.2 Scope labelling

Users must never have to guess what a scope covers. Fixed labels (same strings everywhere: plugin list, bindings table, confirm dialog, resolve preview, key and group pages):

| Scope | Label | Sub-label |
|---|---|---|
| Global | **All keys, all models** | "Default for every request. Groups and keys below can override it." |
| Model Group | **Models in "{group}"** | "{n} models · any key" (plus "linked to {k} keys for access" if linked, else "plugin-only group") |
| API Key | **Key "{key}"** | "Any model this key uses" |

Rules:
- The word "Global" alone is never shown as a scope name. The plugin list column showing the Global state is titled "All keys, all models".
- State options render as **On**, **Off**, **Inherit (use {next broader label})**. For example, the Inherit option on a key row reads "Inherit (use group or All keys, all models)". The Global row has no Inherit option.
- Group rows show member count and a "View models" expander, because group scope follows the requested model, not the key's linked groups.
- **Confirm on enable** (§9.1) for the Global row adds: "This turns {plugin} on for all {k} keys and all {m} models, including keys and models added later." ({k} active keys, {m} enabled models, from memory.)
- The Plugins page header shows a short legend: "Key beats Group beats All keys, all models. Inherit = no opinion, ask the broader scope."
- Global plugin state is a different control from the global model on/off switch (model firewall). The Models page tooltip for the firewall reads "Blocks this model for everyone. Unrelated to plugins."

**Decided by.** Resolve preview and the per-key Plugins section show, for every plugin, the final state and which binding decided it:

| Plugin | Runs | Decided by |
|---|---|---|
| Headroom | On | Models in "Demanding" |
| Caveman | On | Key "pi-dev" |
| Ponytail | Off | All keys, all models (default) |

`GET /api/v1/plugins/resolve` returns this per plugin as `decided_by: {scope_type, scope_id, label}`. When groups were candidates but lost on priority, it also returns `overridden: [{scope_type, scope_id, label, state}]`, shown as a muted "also matched" line.

## 10. Telemetry

- `tokens_saved`: sum of plugin-reported values only. Never estimated.
- `tokens_overhead`: sum of plugin-reported overhead; for Caveman/Ponytail this is a character-based estimate labelled "(est.)".
- `plugin_ms`: wall time of the whole pipeline.
- `plugin_errors`: IDs of plugins that failed (fail-open or fail-closed).
- `plugins_skipped`: `id:reason` entries for idempotency skips (`marker_present`, `already_applied`). Shown in Traffic Explorer detail.
- System log entries (`source=plugin`) for every plugin error with plugin ID, key name, model, and error.

## 11. Security

- HTTP Plugin registration, URL change, secret rotation, deletion, and failure-policy change are admin-only and audited.
- Secrets stored plain text in v1, consistent with provider keys (see §13).
- Secret shown once on create/rotate; masked everywhere else.
- Plugin URL must be `http` or `https`. No other validation in v1 (admin trust boundary). Loopback and private addresses are allowed because Headroom and local plugins live there.
- Client IP never sent to plugins.
- `X-NineGuard-Plugins` header never forwarded upstream.
- `X-NineGuard-Plugins-Applied` can be forged by any client, so it only skips bypassable plugins (§4.9). It reveals which built-ins ran to the upstream provider; acceptable since upstreams are admin-configured.
- Fail-closed plugins cannot be bypassable (enforced server-side).

## 12. Deployment Notes

For the current VM (NineGuard with `--network host`, Headroom in Docker with published port 8787):

- Headroom sees NineGuard's call as non-loopback (Docker bridge) and returns 404. Set `HEADROOM_COMPRESS_ALLOW_REMOTE=1` on the Headroom container.
- Close 8787, 8317, and 20128 in the cloud Security Group. Docker-published ports bypass ufw.
- Turn off RTK in 9router dashboard to avoid double compression. If RTK must stay on, tick "Upstream already applies token saving" on the 9router provider so NineGuard warns.
- A provider that is another NineGuard: built-ins applied here are skipped there automatically (§4.9) as long as they are bypassable on the downstream instance; tick the provider flag if that instance runs other token savers.
- On a 2 vCPU / 2 GB VM, Headroom adds roughly 1–3 s per request. Plugin latency is visible in Traffic Explorer.

These notes go into the README plugin section.

## 13. Related Issues (separate tasks)

1. **Client API keys stored in plain text.** `internal/keys/keys.go` writes `rawKey` to `api_keys.key`, but README says only hashes are stored. Provider API keys and (after this feature) plugin secrets are also plain text. Fix all together: hash client keys (SHA-256 lookup), encrypt provider keys and plugin secrets with `NINEGUARD_SECRET_KEY`.
2. **Requests without `model` skip per-key ACL.** `IsModelAllowed("")` returns `true`; `/v1/embeddings` or similar with missing `model` is forwarded.

## 14. Open Questions

1. **Ponytail level text.** Upstream `AGENTS.md` is one compact prompt; levels `lite/full/ultra` are selected by an instruction line. Confirm during plan whether a single bundled text plus a level line is sufficient.

## 15. Testing

- **Unit**
  - Resolution: global/group/key precedence, `inherit`, group priority, tie-break, pattern matching parity with access control.
  - Settings merge.
  - Worked example §5.5 and its variations as a table-driven test, asserting `decided_by` for each plugin.
  - Scope label helper: Global/group/key labels and Inherit option text.
  - `Warnings`: output-style overlap, upstream flag, input+output combination produces none, `other` category ignored.
  - Idempotency: marker skip in string and array content, `developer` role; applied header parse/union/sort; header ignored for non-bypassable plugins; HTTP IDs never emitted.
  - Pipeline: ordering, fail-open skip, fail-closed 503, reject 403, bypass header respects `bypassable`, model field restored if plugin changes it.
  - Caveman/Ponytail injection: prepend position, deterministic output, client system message untouched.
  - Headroom adapter: `frozen_message_count` calculation, `system`/`tools` preservation, `compression_skipped` handling (with `httptest` server).
  - HTTP plugin contract: secret header, timeout, malformed responses.
- **Integration** (`internal/proxy`)
  - End-to-end chat request through plugins to a fake upstream; verify forwarded body and traffic log columns.
  - Non-chat endpoints bypass plugins.
  - `X-NineGuard-Plugins` stripped before upstream.
  - `X-NineGuard-Plugins-Applied` set on upstream request; two chained proxies apply Caveman once.
- **Handler**
  - RBAC: operator cannot register/delete HTTP plugins or change URL/secret/failure policy.
  - Audit entries written.
  - Built-in deletion rejected.
  - Binding upsert returns scope warnings; `/plugins/warnings` grouping and truncation.
  - Provider `upstream_token_saving` round-trip.
- **Migration**
  - Fresh DB and existing DB both end with seeded built-ins, all global `off`.
- Build checks: `go test ./...` and `CGO_ENABLED=0 go test -tags server ./...`.

## 16. Licensing

- Add `NOTICE` with attributions:
  - Caveman — Apache License 2.0, Copyright 2026 Julius Brussee.
  - Ponytail — MIT License, Copyright 2026 DietrichGebert.
- Bundled prompt files stored under `internal/plugins/builtin/prompts/` with their license headers.
