# Handover — Usage Diagnostics, Estimated Cost & Usage Summary

- Date: 2026-10-19
- From: design session (grilling + domain modeling) with the repo owner
- For: any agent or model picking up this work
- State: **design done, spec in Draft, no plan, no code**

Read this file fully before touching anything. Then read the documents in §3 in order.

---

## 1. What the owner asked for

Three features for NineGuard:

1. **"SDK Anthropic"**: key holders say Claude feels "dumber" through NineGuard than in Claude Code / Claude Desktop. A coworker suggested the Anthropic SDK. The design reframed this: NineGuard speaks only OpenAI `chat/completions`; the upstream (9router) converts to Anthropic Messages, and that conversion likely loses extended thinking, interleaved thinking, `anthropic-beta` headers and prompt caching. Before building native `/v1/messages` support, **measure** it (Phase 1). The decision is Phase 4.
2. **Estimated cost** beside token counts (USD, optionally IDR or another currency).
3. **Usage summary per API Key**, like the `/quota` command, modelled on the Claude Desktop stats screenshot the owner shared (Sessions, Messages, Total tokens, Active days, Peak hour, Favorite model, daily heatmap, per-model chart, "~117,869× more tokens than Animal Farm").

Facts from the owner that shaped the design:

- NineGuards are **chained**: `user -> owner's NineGuard -> company NineGuard -> 9router -> Claude subscription accounts`.
- The complaints come from the **first** (company) NineGuard, which has **no Caveman plugin**. Caveman is on only in the owner's own instance, so Caveman is ruled out as the cause.

## 2. Current state of the repo

Branch: `main`. Nothing committed from this session. Uncommitted:

```
 M GLOSSARY.md                                   (new Reporting terms, /usage note)
 M docs/FUTURE_PLANNED_FEATURES.md               (new §7)
?? docs/adr/0011-proxy-requests-stream-usage.md
?? docs/adr/0012-estimated-cost-stored-per-request.md
?? docs/adr/0013-upstream-usage-aggregate-only.md
?? docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md
?? docs/superpowers/handover/2026-10-19-usage-diagnostics-cost-summary.md   (this file)
```

Suggested commit, once the owner agrees: `docs: usage diagnostics, cost and summary — ADRs 0011-0013, spec, handover`.

**No application code has been changed.**

## 3. Read in this order

1. `AGENTS.md`: mandatory workflow, quality gates, SQLite single-connection rule, CRLF, languages.
2. `GLOSSARY.md` → section **Reporting** (Served Model … Usage Comparison) and **Chat Command Interceptor**. Use these terms exactly in code comments, UI text and docs.
3. ADRs `0011`, `0012`, `0013` in `docs/adr/`. Background: `0008` (Requested Model vs `model_id`), `0009` (Cascaded Quota, chat commands), `0010` (reset schedule / timezones).
4. Spec: `docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md`. Phases, schema, parsing table, pricing formula, endpoints, UI, tests, delivery.
5. Example of the expected plan format: `docs/superpowers/plans/2026-10-17-quota-reset-schedule.md`.

## 4. Workflow you must follow (AGENTS.md §3)

1. **Spec approval.** The spec is `Status: Draft`. Ask the owner to approve it (or change it), especially the items in §6 below. **Do not write application code before the spec and the plan are approved.**
2. **Plan, one per phase.** Write `docs/superpowers/plans/2026-10-19-usage-diagnostics.md` first (Phase 1 only). Use the writing-plans format: header, global constraints, "Spec corrections" section, tasks with exact files, test-first steps, no placeholders. Phases 2 and 3 get their own plans later, after the previous phase ships.
3. **Branch per phase**: `feat/usage-diagnostics`, then `feat/estimated-cost`, then `feat/usage-summary`. Never on `main`. Squash before merge.
4. **Gates** before every commit: `CGO_ENABLED=0 go test -tags server ./...`, `CGO_ENABLED=0 go build -tags server ./...`, `node --test web/jstest/*.mjs` (glob form), `node --check` every edited `.js`, rebuild + restart after UI edits (assets are embedded).
5. If the plan has to deviate from the spec, record it under "Spec corrections" in the plan and apply it to the spec in the last task.
6. Set the spec to `Status: Implemented` only when Phase 3 ships. Update `docs/ARCHITECTURE.md` (Indonesian; keep it Indonesian), `AGENTS.md` (ADR 0011 note), README EN + ID.

## 5. Code map (verified during the session)

| Thing | Where |
|---|---|
| Proxy entry, request flow | `internal/proxy/proxy.go` (`ServeHTTP`) |
| Request struct (only `model`, `stream`, `messages`) | `proxy.go` ~L75 `chatRequest`. **Do not re-marshal it to forward**: it drops every other field. Use a `map[string]json.RawMessage` round-trip for the `include_usage` injection. |
| Usage struct (3 fields only) | `proxy.go` ~L99 `openAIUsage`, `chatResponse` |
| Chat-command interception | `proxy.go` ~L253-284; `internal/proxy/chat_command.go` (`IsChatCommand`, `FormatQuotaMarkdown`, `WriteChatCommandResponse`) |
| `GET /v1/quota` | `proxy.go` ~L207, `buildUnifiedQuotaResponse` |
| Plugin pipeline gate (`isChat`) | `proxy.go` ~L449 |
| Forward + headers | `proxy.go` ~L529-575 (`forwardPath`, `forwardBody`, trace/hop headers) |
| **SSE relay + usage parse + chunk-count guess** | `proxy.go` ~L660-725. Currently relays **line by line**; Phase 1 must relay **event by event** (blank-line delimited) to drop the injected usage-only event. |
| Final traffic record (async goroutine) | `proxy.go` ~L726-760 (`traffic.LogEntry`, payload capture) |
| Other `Record` calls (401, 429, 403, 404, plugin reject, 502, chat command) | `proxy.go` L150, 225, 268, 309, 344, 377, 417, 499, 602 |
| `traffic_logs` schema + migrations | `internal/db/db.go` L77-93, `migrate()` ~L211-290 |
| `LogEntry`, `Record`, list filters (`has_images` example) | `internal/traffic/traffic.go` ~L35-140, L424-460, L520-530 |
| Dashboard stats / reports / tz handling | `traffic.go` `GetDashboardStats` ~L1011, `GetUsageReports` ~L1499, `timeutil.SQLiteOffsetModifier` |
| Settings getters/setters pattern | `internal/traffic/quota.go` (`GetHeavyTokenThreshold`, `IsChatCommandsEnabled`, …) |
| Traffic settings API | `internal/handler/handler.go` ~L1795-1854; filter parsing ~L937-975 |
| Model sync loop (30 min) | `cmd/nineguard/main.go` ~L157-166. Hook price-list and FX refresh here (≤ once per 24h). Routes are also registered in `main.go`. |
| Upstream quota sync | `internal/handler/upstream_quota.go` ~L156 |
| Cascaded NineGuard call (pattern for Upstream Usage) | `internal/upstream/nineguard.go` (URL building, Bearer key, trace headers, cache) |
| Key reset schedule / timezone | `api_keys.quota_reset_schedule` JSON; `keyInfo.ResetRule`; `internal/traffic/schedule.go` |
| UI: traffic badges | `web/static/js/views/traffic.js` `rowEl()` ~L500-588 |
| UI: dashboard top keys card | `web/static/js/views/dashboard.js` ~L671-736 |
| UI: key modal | `web/static/js/views/endpoints.js` `openKeyModal` ~L440; `keyquota.js`, `schedulefields.js` |
| UI charts | `web/static/js/chart.js` (inline SVG, no library) |

Line numbers are from `main` at `d9f1983` and will drift.

## 6. Spec decisions the owner has NOT yet explicitly confirmed

These were made while writing the spec. Raise them when asking for approval:

1. **Quotas will fill faster.** `/v1/messages` and `/v1/responses` traffic is currently counted as a guess or 0. Phase 1 records real totals including cache reads, so keys with quotas will hit their limits sooner. Needs a release note (spec §4.3).
2. **`/usage <provider>` changes meaning.** It used to filter the quota report. It becomes the Usage Summary; `/quota <provider>` keeps the filter (spec §6.6).
3. **Cost is priced on the resolved (requested) Model**, not the Served Model (spec §5.2).
4. **Estimated Usage rows count in token totals but are never priced.** The summary shows "≥ $X" when some requests are unpriced (spec §6.1).
5. **Thinking Requested also triggers on Model IDs containing `thinking`** (spec §4.3).

## 7. Open points to resolve during implementation

- **Assumption to verify before Phase 2 pricing ships:** 9router's `chat` responses report `prompt_tokens` *including* cached tokens (OpenAI convention). Capture a real payload (Payload Inspector, `record_payloads=all`) and check. If it is wrong, the cost formula in spec §5.2 changes.
- **Book word counts** (spec §6.5) must come from cited sources, not from memory. Indonesian titles (*Laskar Pelangi*, *Bumi Manusia*) only with sourced counts.
- Price list facts checked on 2026-10-19: the LiteLLM JSON at `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json` is ~3 MB, 4,221 entries, per-token USD, keys `input_cost_per_token`, `output_cost_per_token`, `cache_read_input_token_cost`, `cache_creation_input_token_cost`, `mode`, `litellm_provider`. It contains a `sample_spec` entry to skip, and Bedrock-style IDs (`us.anthropic.claude-opus-4-6-v1`) that `NormalizeFamily` must collapse.
- Exchange rate checked on 2026-10-19: `https://api.frankfurter.app/latest?from=USD&to=IDR` → `{"amount":1.0,"base":"USD","date":"2026-10-16","rates":{"IDR":18031}}`. No key needed; ECB data, working days only.

## 8. Pitfalls

- **SQLite has one connection.** Close `rows` before the next query; inside a tx use only `tx`. Session inference with `LAG()` must run as one query, not a loop of queries.
- **Don't change access/disabled/quota checks** as a side effect. The quota numbers change because usage parsing gets more accurate; that is intended and documented. The enforcement logic itself must not change.
- **Plugins only run on `chat`.** Phase 1 parses `messages`/`responses` read-only; do not route them through plugins or chat commands.
- **The core changes the request body only as ADR 0011 allows** (`stream_options.include_usage`). Nothing else.
- **Cost is `NULL`, never `0`,** when unknown. Hidden cost fields are **absent** from JSON, not zero.
- **Chat-command rows** (`model = 'nineguard/system'`) are excluded from summaries and never priced.
- CRLF files, minimal diffs, English in code/specs/plans, Indonesian in `ARCHITECTURE.md`.
- Use fake keys in tests (`sk-ng-test…`). Never commit `data/` or `*.db`.

## 9. Suggested first message to the owner

> I've read the handover, ADRs 0011-0013 and the usage spec. Before I write the Phase 1 plan, please confirm or change the five spec decisions in handover §6 (quotas filling faster, `/usage <provider>`, pricing on the requested model, unpriced estimated rows, `thinking` model names). Shall I also commit the docs on `main` first, or on `feat/usage-diagnostics`?

Then write the Phase 1 plan and stop for approval.
