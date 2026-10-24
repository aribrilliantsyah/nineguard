# Usage Diagnostics, Estimated Cost & Usage Summary — Design Spec

- Date: 2026-10-19
- Status: Accepted
- Related: `GLOSSARY.md` (Served Model, Model Fallback, Thinking Requested, Thinking Delivered, Estimated Cost, Display Currency, Estimated Usage, Session, Usage Summary, Upstream Usage, Usage Comparison, Chat Command Interceptor), `docs/adr/0011-proxy-requests-stream-usage.md`, `docs/adr/0012-estimated-cost-stored-per-request.md`, `docs/adr/0013-upstream-usage-aggregate-only.md`, `docs/adr/0009-upstream-quota-tracking-and-interception.md`

## 1. Problem

1. Key holders on chained NineGuards report Claude answering "dumber" than in Claude Code or Claude Desktop. NineGuard cannot tell why: it records no reasoning, cache, or served-model facts, and streamed usage is usually a guess (`proxy.go`: chunk count and bytes / 4).
2. Admins and key holders see tokens but no money. They want an API-equivalent price, in USD or a local currency (IDR).
3. Key holders can see their remaining quota (`/quota`) but not their history: how much, which models, when.

Feature 1 from the original request (an "Anthropic SDK" / native `/v1/messages` support) is not designed here. Phase 4 decides it from Phase 1 data.

## 2. Scope

### In scope

- **Phase 1, Diagnostics**: real streamed usage (ADR 0011); cache read/write, reasoning tokens, Thinking Requested/Delivered, Served Model, Model Fallback, Estimated Usage on `chat/completions`, `messages` and `responses`; Traffic badges, filters, fallback WARN.
- **Phase 2, Estimated Cost**: price list sync with built-in snapshot, per-Model overrides, `cost_usd` at write time (ADR 0012), re-price, exchange rate (auto + manual), Display Currency, cost visibility for key holders.
- **Phase 3, Usage Summary**: `/usage` chat command, `GET /v1/usage`, admin per-key panel, Sessions, Upstream Usage (ADR 0013), Usage Comparison.
- **Phase 4**: decision gate only.

### Out of scope

- Native `/v1/messages` handling (plugins, chat commands, format conversion). Phase 4.
- A force-thinking plugin. Phase 4.
- Real spend from subscriptions (GLOSSARY: Estimated Cost).
- Tiered pricing (e.g. above 200k context), batch prices, per-region prices, web-search fees.
- Per-user attribution at a parent NineGuard (ADR 0013 option 3).
- Leaderboards or comparisons between API Keys visible to key holders.
- Admin-editable book list.
- Backfilling diagnostics for old rows (the facts were never captured).

## 3. Schema

All in `migrate()` (`internal/db/db.go`), idempotent `ALTER TABLE … ADD COLUMN`, no backfill unless stated.

### 3.1 `traffic_logs` (Phase 1 unless noted)

```sql
ALTER TABLE traffic_logs ADD COLUMN api_format          TEXT    DEFAULT '';  -- 'chat' | 'messages' | 'responses' | 'other' | ''
ALTER TABLE traffic_logs ADD COLUMN cache_read_tokens   INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN cache_write_tokens  INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN reasoning_tokens    INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN thinking_requested  INTEGER DEFAULT 0;   -- 0/1
ALTER TABLE traffic_logs ADD COLUMN thinking_delivered  INTEGER DEFAULT 0;   -- 0/1, 1 only on evidence
ALTER TABLE traffic_logs ADD COLUMN served_model        TEXT    DEFAULT '';
ALTER TABLE traffic_logs ADD COLUMN model_fallback      INTEGER DEFAULT 0;   -- 0/1
ALTER TABLE traffic_logs ADD COLUMN usage_estimated     INTEGER DEFAULT 0;   -- 0/1
ALTER TABLE traffic_logs ADD COLUMN cost_usd            REAL;                -- Phase 2, NULL = no Estimated Cost
```

Rows written before migration read `usage_estimated = 0`; the UI shows diagnostics columns as "—" for rows with `api_format = ''` (pre-feature).

### 3.2 Prices (Phase 2)

```sql
CREATE TABLE IF NOT EXISTS model_prices (          -- from the price list; replaced wholesale on refresh
  family            TEXT PRIMARY KEY,               -- normalized family (§4.4)
  source_key        TEXT NOT NULL,                  -- original price-list key, for display
  input_per_mtok    REAL NOT NULL,                  -- USD per 1M tokens
  output_per_mtok   REAL NOT NULL,
  cache_read_per_mtok  REAL,                        -- NULL = not published
  cache_write_per_mtok REAL,
  updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS model_price_overrides (  -- admin, per exact Model
  model_id          TEXT PRIMARY KEY,               -- models.id
  input_per_mtok    REAL NOT NULL,
  output_per_mtok   REAL NOT NULL,
  cache_read_per_mtok  REAL,
  cache_write_per_mtok REAL,
  updated_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Per-million rates are stored (not per-token) so admins read and type familiar numbers.

### 3.3 `api_keys` (Phase 2)

```sql
ALTER TABLE api_keys ADD COLUMN cost_visibility TEXT;  -- NULL = follow global, 'show', 'hide'
```

### 3.4 `settings` keys

| Key | Phase | Default | Meaning |
|---|---|---|---|
| `stream_usage_injection` | 1 | `true` | ADR 0011 on/off |
| `price_list_url` | 2 | LiteLLM raw URL (§5.1) | Price source |
| `price_list_synced_at` | 2 | — | Last successful refresh |
| `display_currency` | 2 | `USD` | ISO 4217 code |
| `fx_rate_manual` | 2 | empty | Number; when set, wins |
| `fx_rate_auto` | 2 | — | JSON `{"currency":"IDR","rate":18031,"date":"2026-10-16","fetched_at":"…"}` |
| `cost_visible_to_keys` | 2 | `true` | Global cost visibility |
| `session_gap_minutes` | 3 | `30` | Inferred Session gap |
| `usage_comparison_enabled` | 3 | `true` | Book line on/off |

## 4. Phase 1 — Diagnostics

### 4.1 API format

Set from the request path after `/v1` is stripped: suffix `/chat/completions` → `chat`, `/messages` → `messages`, `/responses` → `responses`, any other proxied POST → `other` (no parsing beyond today's).

### 4.2 Stream usage injection (ADR 0011)

On `api_format = chat`, `stream: true`, setting on:

- If `stream_options.include_usage` is not `true`, set it in `forwardBody` (after the plugin pipeline), preserving unknown fields (`map[string]json.RawMessage` round-trip, not `chatRequest`). Remember `injectedUsage = true`.
- SSE relay changes from line-by-line to event-by-event (an event ends at a blank line), so a whole event can be dropped. When `injectedUsage`, an event whose JSON has `choices` as an empty array and a non-null `usage` is parsed but not written. Any event with content is written unchanged.
- Flushing per event keeps latency equal to today for normal chunks.

### 4.3 Usage, reasoning, thinking, served model by format

All counts are normalized to OpenAI meaning: `prompt_tokens` includes cache reads and writes; `completion_tokens` includes reasoning; `total = prompt + completion`.

| | `chat` | `messages` (Anthropic) | `responses` |
|---|---|---|---|
| Usage source | `usage` (body / last usage chunk) | `message_start.message.usage`, then `message_delta.usage` (output is cumulative, last wins) | `response.completed` → `response.usage` |
| prompt | `prompt_tokens` | `input_tokens + cache_read_input_tokens + cache_creation_input_tokens` | `input_tokens` |
| completion | `completion_tokens` | `output_tokens` | `output_tokens` |
| cache read | `prompt_tokens_details.cached_tokens` | `cache_read_input_tokens` | `input_tokens_details.cached_tokens` |
| cache write | `prompt_tokens_details.cache_write_tokens`, else `cache_creation_input_tokens` if present | `cache_creation_input_tokens` | — |
| reasoning | `completion_tokens_details.reasoning_tokens` | — (not reported) | `output_tokens_details.reasoning_tokens` |
| Thinking Delivered evidence | non-empty `reasoning_content`, `reasoning`, or `reasoning_details` in `message`/`delta`; or reasoning tokens > 0 | content block of type `thinking` or `redacted_thinking` (body or `content_block_start`) | output item of type `reasoning`; or reasoning tokens > 0 |
| Served Model | top-level `model` (first chunk that has one) | `message.model` | `response.model` |

**Thinking Requested** (request body, after plugins):

- `chat`: `reasoning_effort` present and not `none`; or `reasoning` object with `enabled: true`, `effort` not `none`, or `max_tokens > 0`; or `thinking.type` in `enabled`/`adaptive` (some routers accept it).
- `messages`: `thinking.type` in `enabled`/`adaptive`.
- `responses`: `reasoning.effort` present and not `none`.
- Any format: resolved Model ID contains `thinking` (router-exposed thinking variants).

Thinking Delivered is `0` when there is no evidence. It is never inferred from latency.

**Estimated Usage**: set when today's chunk-count guess is used, or when a 2xx non-stream response has no usage. The guess itself is unchanged.

**Important consequence:** `messages` and `responses` traffic has no `total_tokens` today, so it is recorded as a guess (stream) or 0 (non-stream). After Phase 1 it records real totals including cache reads, which for agent traffic can be very large. Key Quotas and Model Quotas on such keys will fill much faster. The release note must say so.

### 4.4 Family normalization (shared with Phase 2)

`NormalizeFamily(id string) string`, in `internal/models` (pure function):

1. Lowercase, trim.
2. Keep the segment after the last `/` (`nineguard/9router/claude-opus-4.5` → `claude-opus-4.5`; `openrouter/anthropic/claude-opus-4.5` → `claude-opus-4.5`).
3. Drop a leading cloud/region prefix: `anthropic.`, `us.`, `eu.`, `apac.`, `au.`, `global.` (repeatable).
4. Drop suffixes: `-v\d+:\d+`, `@\d{8}`, `-\d{8}`, `-\d{4}-\d{2}-\d{2}`, `-latest`, `:thinking`, `-thinking`, `:free`, `:beta`.
5. Replace `.` and `_` with `-`.

Examples: `claude-opus-4-5-20251101` and `claude-opus-4.5` → `claude-opus-4-5`. `gpt-4o-2024-08-06` → `gpt-4o`.

**Model Fallback** = Served Model non-empty and `NormalizeFamily(served) != NormalizeFamily(resolved Model ID)`. Word-order aliases (`claude-3-5-sonnet` vs `claude-sonnet-3-5`) are not unified; a false positive is visible and harmless.

On fallback: `slog.Warn` with `source=traffic`, `model_fallback: key "<name>" requested <model_id>, served <served>`, at most once per (key, model, served) per 10 minutes (in-memory).

### 4.5 Traffic API and UI

- `LogEntry`, `Record`, list and detail queries carry the new columns.
- New filters on `GET /api/v1/traffic`: `api_format`, `thinking=requested|delivered|lost` (`lost` = requested and not delivered), `fallback=1`, `estimated=1`.
- `traffic.js` row badges, beside Heavy and image badges:
  - `🧠 2.1k` thinking delivered (reasoning tokens if known, else `🧠`).
  - `🧠✗` grey: requested, not observed. Tooltip: "Thinking was requested but the upstream response shows no reasoning."
  - `⚠ fallback` with tooltip `requested X → served Y`.
  - `≈` on the token cell when Estimated Usage.
  - Cache column: `cache_read / prompt` as %, blank when prompt is 0.
- Detail modal: API format, Served Model, the five token counts, thinking flags.
- Dashboard: no new cards in Phase 1.

## 5. Phase 2 — Estimated Cost

### 5.1 Price list

- Default `price_list_url`: `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json` (≈3 MB, 4k+ entries, per-token USD; keys `input_cost_per_token`, `output_cost_per_token`, `cache_read_input_token_cost`, `cache_creation_input_token_cost`).
- Import: entries with `mode` in `chat`/`responses` and both input and output costs. Converted to per-1M. Grouped by `NormalizeFamily(key)`. When several keys share a family, prefer `litellm_provider` in this order: `anthropic`, `openai`, `gemini`, `xai`, `deepseek`, `mistral`, `groq`, then any other; ties → shortest key. `sample_spec` is skipped.
- Refresh: from the existing 30-minute model-sync loop (`cmd/nineguard/main.go`), at most once per 24h, plus a manual refresh endpoint. Fetch timeout 30s. Failure keeps current rows and logs WARN. A successful import replaces `model_prices` in one transaction.
- **Built-in snapshot**: a trimmed JSON (family → four rates) embedded with `go:embed` in `internal/pricing`, generated by `scripts/update-prices` (Go program, same import rules). Loaded into `model_prices` on first start when the table is empty, and used when the fetch never succeeded.

### 5.2 Price resolution and cost

`internal/pricing.Resolver.PriceFor(modelID string) (*Price, source string)`, in-memory, reloaded after refresh or override edits:

1. `model_price_overrides` row for the exact Model ID → source `override`.
2. `model_prices` row for `NormalizeFamily(modelID)` → source `list`.
3. None.

The resolved Model ID is priced, not the Served Model: the client is charged for what it asked for, and fallbacks stay visible as a separate fact.

```
uncached = max(0, prompt - cache_read - cache_write)
cost = uncached     * input
     + cache_read   * (cache_read_rate  ?? input)
     + cache_write  * (cache_write_rate ?? input)
     + completion   * output                       (all rates per token = per_mtok / 1e6)
```

`cost_usd` is `NULL` when: no price; `usage_estimated = 1`; total tokens 0; chat-command rows (`nineguard/system`).

### 5.3 Re-price (ADR 0012)

`POST /api/v1/costs/reprice` `{"from":"…","to":"…","model_id":"optional"}` → `{"updated": N}`. Recomputes from stored token columns with current prices, skipping `usage_estimated = 1`. Rows from before Phase 1 have no cache split, so they are priced as all-uncached input; the dialog says so. Writes a syslog INFO with range, model and count.

### 5.4 Exchange rate

- When `display_currency` is not `USD` and `fx_rate_manual` is empty: fetch `https://api.frankfurter.app/latest?from=USD&to=<CUR>` once per 24h (same loop), and on currency change. Store into `fx_rate_auto`. Failure keeps the last rate and logs WARN.
- Effective rate: manual if set, else auto if its currency matches, else none → costs shown in USD with a "no rate for IDR" hint.
- Currency list in the UI: Frankfurter's supported set (fetched once with `/currencies`, built-in fallback list). A manual rate allows any ISO code.
- Formatting: browser `Intl.NumberFormat(locale, {style:'currency', currency})`; Go side (chat command, `/v1/usage`) uses a small table: `USD $1,234.56`, `IDR Rp 4.231.000` (0 decimals, dot thousands), `EUR €`, `JPY ¥` (0 decimals), `SGD S$`, else `<CODE> 1,234.56`. Amounts under 0.01 USD-equivalent show `<$0.01` (or `<Rp 1`).
- Converted totals use today's rate (ADR 0012); tooltip: "1 USD = Rp 18.031 (ECB, 16 Oct) · converted at today's rate".

### 5.5 Visibility to key holders

`ShowCost(key) = key.cost_visibility == 'show' || (key.cost_visibility IS NULL && setting cost_visible_to_keys)`. When false, `/usage`, `/v1/usage`, and Upstream Usage omit cost fields entirely (absent, not 0). Admin views always show cost.

### 5.6 Admin API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/prices` | Every Model with effective price, source (`override`/`list`/`none`), family |
| PUT | `/api/v1/prices/overrides/{model_id}` | Set override (input/output required, cache rates optional) |
| DELETE | `/api/v1/prices/overrides/{model_id}` | Remove override |
| POST | `/api/v1/prices/refresh` | Refresh now; returns count and errors |
| GET/POST | `/api/v1/settings/cost` | `display_currency`, `fx_rate_manual`, `cost_visible_to_keys`, read-only `fx_rate_auto`, `price_list_synced_at`, `price_list_url` |
| POST | `/api/v1/costs/reprice` | §5.3 |

Key create/update accept `cost_visibility` (`null`/`show`/`hide`).

### 5.7 UI

- **Models page**: Price column (`$5 / $25 per 1M`, cache rates in tooltip), source chip, edit dialog for override, "No price" chip for `none`.
- **Settings** (traffic settings card): Display Currency select, manual rate input, rate status line, cost visibility toggle, price list status + Refresh button, Re-price dialog.
- **Traffic**: Cost column (Display Currency), `—` for NULL.
- **Dashboard**: KPI "Est. cost" with previous-period delta; top keys card gains a cost column.
- **Reports**: cost column per key and per model.
- **Key modal**: cost visibility select (Follow global / Show / Hide).
- Every cost label says "Est." with tooltip "API list-price equivalent, not actual spend".

## 6. Phase 3 — Usage Summary

### 6.1 Computation

`traffic.GetUsageSummary(keyID string, rng Range, loc *time.Location, now time.Time) (*UsageSummary, error)`.

- **Ranges**: `7d`, `30d` = the last N local calendar days including today; `all` = since the key's first row.
- **Row set**: the key's rows in range, excluding chat-command rows (`model = 'nineguard/system'`).
- **Requests** = rows with `status_code < 400`. **Errors** = rows with `status_code >= 400` (blocked and failed), shown with % of all rows.
- **Tokens**: sums of prompt, completion, total, cache read, cache write, reasoning over successful rows. Estimated rows are included in tokens (as quotas do) and counted in `estimated_requests`.
- **Cost**: `SUM(cost_usd)`, plus `unpriced_requests` (successful rows with NULL cost). If any, cost shows as "≥ $X".
- **Cache hit %** = cache read / prompt.
- **Sessions**: rows ordered by timestamp; a session starts at the first row and at every row whose gap from the previous exceeds `session_gap_minutes` (SQLite `LAG()`). Label "Sessions (inferred)".
- **Active days**: distinct local dates with at least one row.
- **Peak hour**: local hour with most rows (requests, not tokens); ties → earliest.
- **Favorite model**: by total tokens, `model_id` (fallback `model` for old rows).
- **Thinking**: requested count, delivered count, lost count; **Fallbacks**: count.
- **Daily**: per local date, `{date, requests, tokens, cost}`. `all` caps at the last 371 days (53 weeks).
- **Models**: per model `{model_id, prompt, completion, total, cost, pct_tokens, daily[{date, total}]}`, top 5 by tokens plus `other`.
- **Comparison** (§6.5).

Local dates use `timeutil.SQLiteOffsetModifier` like the dashboard.

### 6.2 Timezone

- `/usage` and `GET /v1/usage`: the key's Effective Reset Schedule timezone (UTC when default).
- Admin panel: viewer's `?tz=` like other dashboard calls.

### 6.3 Endpoints

**`GET /v1/usage?range=7d|30d|all`** (key auth, intercepted beside `/v1/quota`, default `30d`, bad value → 400):

```json
{
  "range": "30d", "from": "2026-09-20", "to": "2026-10-19", "timezone": "Asia/Jakarta",
  "requests": 1204, "errors": 31, "sessions": 43, "active_days": 12, "peak_hour": 0,
  "tokens": {"prompt": 0, "completion": 0, "total": 0, "cache_read": 0, "cache_write": 0, "reasoning": 0},
  "estimated_requests": 0,
  "cache_hit_pct": 91.2,
  "thinking": {"requested": 0, "delivered": 0, "lost": 0},
  "fallbacks": 0,
  "favorite_model": "9router/claude-opus-4.5",
  "cost": {"usd": 123.45, "display": 2225900, "currency": "IDR", "rate": 18031, "unpriced_requests": 0},
  "daily": [], "models": [],
  "comparison": {"book": "Animal Farm", "multiple": 117869.2},
  "upstream_usage": [{"provider": "comp-nineguard", "summary": { /* same shape, from the parent */ }, "error": ""}]
}
```

`cost` and `upstream_usage` are absent when ShowCost is false. `comparison` is absent when disabled.

**`GET /api/v1/keys/{id}/usage?range=…&tz=…`** (admin): same shape, always with cost, without `upstream_usage` (admins see the parent separately on the Upstream Quotas page; not in this spec).

### 6.4 Upstream Usage (ADR 0013)

- For each active Provider with `quota_type = nineguard`: `GET <base>/v1/usage?range=<same>` with the Provider's key, trace/hop headers as `nineguard.go` does. Timeout 5s. Cached 5 min per (provider, range).
- 404 (older parent) or error → entry with `error` set; the chat command prints one muted line or nothing for 404.
- The parent's own visibility rules apply: if it omits `cost`, the child shows tokens only. A parent's USD cost is converted with the child's rate.

### 6.5 Usage Comparison

- `internal/traffic/books.go`: built-in table `{title, words}`; tokens = `words * 4 / 3`. Initial set (word counts to be verified against sources at implementation time, not taken from this spec): Animal Farm, The Little Prince, Harry Potter and the Philosopher's Stone, The Hobbit, Pride and Prejudice, Moby-Dick, The Lord of the Rings (all three), War and Peace, the Bible (KJV), English Wikipedia (approx. 4.5B words).
- Pick: the largest book with `tokens <= total`; `multiple = total / book_tokens`. Below the smallest book: fraction of the smallest (`~40% of Animal Farm`).
- Text: `You've used ~117,869× more tokens than Animal Farm.` Multiple formatted with thousands separators, 1 decimal under 10.
- Same rule everywhere, so the line is stable for a given total.

### 6.6 Chat command

- `IsChatCommand` unchanged in matching. In `proxy.go`, `/usage` routes to `FormatUsageMarkdown`; `/quota` and `/limits` keep `FormatQuotaMarkdown`.
- Args: `7d`, `30d`, `all`; none → `30d`; anything else → `30d` with a first line `Unknown range "x", showing 30d. Use /usage 7d|30d|all.`
- **Behaviour change**: `/usage <provider>` used to filter the quota report. It no longer does; `/quota <provider>` still does. Release note.

Example (cost visible, IDR):

```
📈 NineGuard Usage — last 30 days (Asia/Jakarta)

Requests 1,204 · Errors 31 (2.5%) · Sessions 43 (inferred) · Active days 12
Tokens 4.6B (in 1.2B · out 7.0M · cache hit 91%) · Est. cost Rp 2.225.900
Peak hour 00:00 · Favorite model claude-opus-4.5
Thinking: requested 812 · delivered 790 · lost 22 · Fallbacks 3

| Model | In | Out | Est. cost | Share |
|---|---|---|---|---|
| claude-opus-4.5 | 428.1k | 3.7M | Rp 1.500.000 | 69.9% |
| … | | | | |

You've used ~117,869× more tokens than Animal Farm.

Upstream (comp-nineguard, shared by all users of this gateway): 9.1B tokens · Est. cost Rp 4.100.000
```

Daily heatmap is not rendered in chat.

### 6.7 Admin UI

- Endpoints/keys view: a "Usage" action per key opens a panel (new `views/keyusage.js`), styled after the reference screenshot:
  - Tabs **Overview** / **Models**; range pills **All / 30d / 7d**.
  - Overview: KPI grid (Requests, Sessions, Total tokens, Est. cost, Active days, Peak hour, Favorite model, Cache hit, Thinking lost, Fallbacks), daily heatmap (weeks as columns, weekdays as rows, 5 intensity steps by tokens), comparison line.
  - Models: stacked daily bars (existing `chart.js` SVG helpers), legend `model · in · out · cost · %`.
- Settings: `session_gap_minutes`, `usage_comparison_enabled`.

## 7. Phase 4 — Decision gate (no code)

After ≥ 1 week of Phase 1 data on the first NineGuard (no caveman), check per format for Claude Models:

| Observation | Conclusion | Next |
|---|---|---|
| Thinking Requested rare on `chat` | Clients do not ask | Spec a force-thinking Built-in Plugin |
| Requested often, Delivered rare on `chat`, healthy on `messages` | Lost in OpenAI→Anthropic translation upstream | Spec native `/v1/messages` support |
| Fallbacks frequent | Upstream pool downgrades | Upstream/9router configuration, not NineGuard |
| Cache hit near 0 for agent keys | Caching lost upstream or broken by a plugin | Investigate per plugin binding |

## 8. Documentation updates

- `docs/ARCHITECTURE.md` (Indonesian, keep the language): request lifecycle gains `include_usage` injection, event-based SSE relay, usage normalization, pricing at record time.
- `AGENTS.md`: note that the proxy core changes request bodies only per ADR 0011.
- `docs/FUTURE_PLANNED_FEATURES.md` §7 (done with this spec).
- `GLOSSARY.md` (done).
- README (EN and ID): `/usage`, `GET /v1/usage`, cost settings.

## 9. Testing

Go tests with in-memory SQLite, table-driven, like existing `*_test.go`.

**Phase 1**

- Injection: absent / `false` / `true` `include_usage`; non-stream untouched; setting off untouched; unknown body fields preserved byte-equivalently in meaning.
- SSE relay: usage-only chunk stripped only when injected; content chunk with usage forwarded; multi-line events; events split across reads; `[DONE]` forwarded; flush per event.
- Parsing fixtures per format (non-stream and stream): OpenAI, OpenRouter (reasoning + `cache_write_tokens`), 9router chat, Anthropic Messages with thinking blocks and cache, Responses with reasoning item.
- Thinking Requested matrix (each trigger, `none` values, `-thinking` model).
- `NormalizeFamily` cases from §4.4, Bedrock IDs, nested prefixes, idempotence (`f(f(x)) == f(x)`).
- Fallback: same family different date → false; Opus→Sonnet → true; empty served → false; WARN rate limit.
- Estimated Usage set on guess and on 2xx-without-usage; not set on errors.
- Migration idempotent; Traffic filters return the right rows.
- Quota usage after Phase 1 counts normalized `messages` totals (regression test documenting the intended jump).

**Phase 2**

- Import fixture (trimmed LiteLLM JSON): mode filter, family grouping, provider preference, per-1M conversion, `sample_spec` skipped.
- Fetch failure keeps rows; empty table seeds from snapshot.
- Resolution order override > list > none; reload after edit.
- Cost formula: no cache, cache read only, cache write only, missing cache rates fall back to input, prompt < cache (clamp), NULL cases.
- Re-price: range and model filters, skips estimated, count returned, history outside range untouched.
- FX: manual wins; auto stored; failure keeps last; currency change triggers fetch; formatting table (USD, IDR, JPY, unknown code, < 0.01).
- Visibility: global × per-key matrix; fields absent (not 0) when hidden.

**Phase 3**

- Summary over fixed rows and `now`: ranges, local-day boundaries in `Asia/Jakarta` and `America/New_York` (DST), errors vs requests, chat-command rows excluded, unpriced count.
- Sessions: gaps exactly at, below, above the threshold; single row; setting change.
- Peak hour ties; favorite model by tokens not requests.
- Comparison: below smallest, exactly equal, between books, above largest.
- `/usage` args, unknown arg note, `/quota` and `/limits` unchanged, `/quota <provider>` still filters.
- `GET /v1/usage`: auth, 400 on bad range, cost hidden, upstream entry for 200 / 404 / timeout (httptest parent), cache TTL.
- Admin endpoint uses `tz`, always has cost.

**UI** — manual, both themes: badges and filters on mixed traffic; Models page override; currency switch USD → IDR → manual rate; key usage panel all tabs and ranges; empty key (no traffic).

**JS tests** (`node --test web/jstest/*.mjs`): heatmap bucketing, currency formatter wrapper, badge selection.

## 10. Delivery

Three branches, each shippable alone, each green (`go test`, `go build`, JS tests) before the next. Spec status → `Implemented` when Phase 3 ships.

1. `feat/usage-diagnostics` — §3.1 (minus `cost_usd`), §4, ADR 0011, `ARCHITECTURE.md` lifecycle.
2. `feat/estimated-cost` — §3.1 `cost_usd`, §3.2, §3.3, §5, ADR 0012.
3. `feat/usage-summary` — §6, ADR 0013, README.

## 11. Open points

- Indonesian titles for the book table (e.g. *Laskar Pelangi*, *Bumi Manusia*): include only with sourced word counts; otherwise skip.
- Whether `prompt_tokens` from 9router's `chat` responses already includes cached tokens is assumed (OpenAI convention) and must be confirmed against a captured payload before Phase 2 pricing ships.
