# Implementation Plan: Phase 2 — Estimated Cost

- **Goal:** Implement write-time API-equivalent list cost estimation in USD per request, optional FX conversion (e.g. IDR) via Frankfurter / manual rate, LiteLLM pricing list with overrides, background auto-sync, re-pricing engine, and UI cost reporting across Dashboard, Reports, Traffic Explorer, and Models.
- **Spec Reference:** `docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md` §5.
- **ADR Reference:** `docs/adr/0012-estimated-cost-stored-per-request.md`.
- **Target Branch:** `feat/estimated-cost` (branched from `feat/usage-diagnostics`).

---

## Global Constraints & Principles

1. **Write-Time Pricing (ADR 0012):** Cost is computed once at proxy request completion and stored in `traffic_logs.cost_usd`. It is never recomputed on read. Rows with `usage_estimated = 1`, no matched price, 0 total tokens, or `nineguard/system` rows store `NULL`.
2. **Price Requested Model:** Price the resolved model ID requested by the client, NOT the served model. Fallbacks are separately tracked via `model_fallback`.
3. **Display Currency (ADR 0012):** `traffic_logs.cost_usd` is ALWAYS in USD (`REAL`). Display currency conversion happens at display/API time using the effective rate.
4. **Visibility Control:** `ShowCost(key)` controls whether non-admin surfaces (`/usage`, `/v1/usage`, Upstream Usage) expose cost. Admin views always show cost.
5. **SQLite Single Connection:** Single open connection (`SetMaxOpenConns(1)`). When querying before updating (e.g. Re-price), read all rows into memory, close `rows`, then run updates in a transaction.
6. **Embedded Snapshot:** Embedded fallback JSON (`internal/pricing/prices_snapshot.json`) seeded on first run if `model_prices` is empty.
7. **Quality Gates:**
   - `CGO_ENABLED=0 go test -tags server ./...`
   - `CGO_ENABLED=0 go build -tags server ./...`
   - `node --test web/jstest/*.mjs`
   - `node --check web/static/js/*.js web/static/js/views/*.js`

---

## Tasks

### Task 1: Pricing Data Types, Resolution Math, and Cost Calculator

**Files:**
- Create: `internal/pricing/pricing.go`
- Create: `internal/pricing/pricing_test.go`
- Create: `internal/pricing/resolver.go`
- Create: `internal/pricing/resolver_test.go`

**Produces:**
- Structs: `Price` (`InputPerMTok`, `OutputPerMTok`, `CacheReadPerMTok`, `CacheWritePerMTok`), `ModelPriceRow`, `OverrideRow`.
- Pure math function: `CalculateCostUSD(price *Price, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int) float64`.
  - Uncached prompt: `max(0, prompt - cache_read - cache_write)`.
  - Cache read rate: `price.CacheReadPerMTok` if present, else fallback to `InputPerMTok`.
  - Cache write rate: `price.CacheWritePerMTok` if present, else fallback to `InputPerMTok`.
  - Total: `(uncached * input + cache_read * cache_read_rate + cache_write * cache_write_rate + completion * output) / 1e6`.
- `Resolver` struct with thread-safe in-memory cache:
  - `PriceFor(modelID string) (price *Price, source string, found bool)`.
  - Precedence: (1) `model_price_overrides` for exact model ID -> `source = "override"`; (2) `model_prices` for `models.NormalizeFamily(modelID)` -> `source = "list"`; (3) not found -> `found = false`.
  - `Reload(prices map[string]Price, overrides map[string]Price)` to safely swap in-memory maps.

- [x] **Step 1: Write failing unit tests for cost math and resolver**
  In `internal/pricing/pricing_test.go`:
  - CalculateCostUSD with standard prompt and completion tokens.
  - CalculateCostUSD with prompt cache read and write tokens (custom rates).
  - CalculateCostUSD with nil cache rates falling back to input rate.
  - CalculateCostUSD with cache read + write exceeding prompt tokens (uncached clamped to 0).
  In `internal/pricing/resolver_test.go`:
  - Resolver returns override price with `source="override"` when model ID matches.
  - Resolver returns list price with `source="list"` when family matches (e.g. `claude-3-5-sonnet-20241022` resolves to `claude-3-5-sonnet`).
  - Resolver returns `found=false` when neither matches.
  - Override takes priority over list price for the same model.
  Run `go test ./internal/pricing` -> FAIL (packages not created).

- [x] **Step 2: Implement pricing types, calculator, and resolver**
  Implement `internal/pricing/pricing.go` and `internal/pricing/resolver.go`.

- [x] **Step 3: Run unit tests**
  `CGO_ENABLED=0 go test -tags server ./internal/pricing` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/pricing/`
  `git commit -m "feat(pricing): core price structures, cost calculation, and memory resolver"`

---

### Task 2: LiteLLM Price Importer, Preference Order, and Embedded Snapshot

**Files:**
- Create: `internal/pricing/importer.go`
- Create: `internal/pricing/importer_test.go`
- Create: `internal/pricing/prices_snapshot.json`
- Create: `scripts/update-prices/main.go`

**Produces:**
- Importer parser: `ParseLiteLLM(data []byte) (map[string]Price, error)`.
  - Mode filter: `mode` in `chat` or `responses`.
  - Required fields: `input_cost_per_token > 0` and `output_cost_per_token > 0`.
  - Token conversion: multiply per-token USD by `1e6` to store per-1M tokens.
  - Family grouping: group by `models.NormalizeFamily(modelKey)`.
  - Conflict resolution: when multiple models share a normalized family, prefer `litellm_provider` in order: `anthropic`, `openai`, `gemini`, `xai`, `deepseek`, `mistral`, `groq`, then others. Break ties with shortest key length.
  - Skip `sample_spec`.
- Embedded snapshot:
  - `//go:embed prices_snapshot.json` in `internal/pricing`.
  - `LoadSnapshot() (map[string]Price, error)`.
- Generator script `scripts/update-prices/main.go` to download or parse LiteLLM JSON and output `prices_snapshot.json`.

- [x] **Step 1: Write failing importer tests with fixture**
  In `internal/pricing/importer_test.go`:
  - Test trimmed LiteLLM JSON fixture containing `sample_spec`, non-chat modes (e.g. `embedding`), missing output costs, multiple providers for same family (`anthropic/claude-3-5-sonnet` vs `openrouter/...`), and cache rates.
  - Verify rates are multiplied by 1,000,000.
  - Verify provider preference order selects `anthropic` over other providers.
  - Verify `LoadSnapshot` successfully loads non-empty embedded prices map.
  Run `go test ./internal/pricing` -> FAIL.

- [x] **Step 2: Implement importer and generate snapshot**
  - Implement `internal/pricing/importer.go`.
  - Create `scripts/update-prices/main.go`.
  - Run script or generate `prices_snapshot.json` containing initial major model families (Claude 3/3.5/3.7/Opus/Sonnet/Haiku, GPT-4o/4o-mini/o1/o3-mini, Gemini 1.5/2.0/Flash/Pro, DeepSeek V3/R1, Mistral, Grok).

- [x] **Step 3: Run importer tests**
  `CGO_ENABLED=0 go test -tags server ./internal/pricing` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/pricing/ scripts/update-prices/`
  `git commit -m "feat(pricing): litellm price importer, provider preference, and embedded snapshot"`

---

### Task 3: FX Exchange Rate Client, Currency Formatting, and Visibility Logic

**Files:**
- Create: `internal/pricing/fx.go`
- Create: `internal/pricing/fx_test.go`
- Create: `internal/pricing/format.go`
- Create: `internal/pricing/format_test.go`
- Create: `internal/pricing/visibility.go`
- Create: `internal/pricing/visibility_test.go`

**Produces:**
- FX Client:
  - `FetchFrankfurterRate(ctx context.Context, toCurrency string) (rate float64, err error)`.
  - Supported currency fallback list.
  - Effective rate math: manual rate > 0 takes priority; otherwise auto rate if currency matches; otherwise 1.0 (USD).
- Go Cost Formatter:
  - `FormatCost(costUSD float64, currency string, rate float64, unpricedRequests int64) string`.
  - Supports zero-decimal currencies: `IDR` (`Rp 4.231.000`), `JPY` (`¥1,234`).
  - Supports standard currencies: `USD` (`$1,234.56`), `EUR` (`€1,234.56`), `SGD` (`S$1,234.56`), fallback `<CUR> 1,234.56`.
  - Formats `<$0.01` (or `<Rp 1`) for positive amounts below minimum display unit.
  - Prepends `≥ ` if `unpricedRequests > 0`.
- Visibility helper:
  - `ShowCost(keyCostVisibility *string, globalCostVisible bool) bool`.
  - `keyCostVisibility == "show"` -> true.
  - `keyCostVisibility == "hide"` -> false.
  - `nil` or `""` -> falls back to `globalCostVisible`.

- [x] **Step 1: Write failing tests for FX, formatting, and visibility**
  In `internal/pricing/fx_test.go`: mock HTTP server returning Frankfurter response; test manual override precedence; test fallback when currency is USD.
  In `internal/pricing/format_test.go`: test IDR dot-thousands and zero decimals; test USD comma-thousands and cents; test sub-cent amounts; test `≥ ` prefix with unpriced requests.
  In `internal/pricing/visibility_test.go`: test matrix for key override vs global setting.
  Run `go test ./internal/pricing` -> FAIL.

- [x] **Step 2: Implement FX, formatting, and visibility**
  Implement `internal/pricing/fx.go`, `format.go`, and `visibility.go`.

- [x] **Step 3: Run unit tests**
  `CGO_ENABLED=0 go test -tags server ./internal/pricing` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/pricing/`
  `git commit -m "feat(pricing): fx exchange client, currency formatting, and visibility evaluation"`

---

### Task 4: Database Migrations, Settings, and Pricing Manager

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/cost_migration_test.go`
- Modify: `internal/keys/keys.go`
- Modify: `internal/keys/keys_test.go`
- Create: `internal/pricing/manager.go`
- Create: `internal/pricing/manager_test.go`

**Produces:**
- DB migrations in `internal/db/db.go`:
  - `CREATE TABLE IF NOT EXISTS model_prices (family TEXT PRIMARY KEY, input_per_mtok REAL NOT NULL, output_per_mtok REAL NOT NULL, cache_read_per_mtok REAL, cache_write_per_mtok REAL, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
  - `CREATE TABLE IF NOT EXISTS model_price_overrides (model_id TEXT PRIMARY KEY, input_per_mtok REAL NOT NULL, output_per_mtok REAL NOT NULL, cache_read_per_mtok REAL, cache_write_per_mtok REAL, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`
  - `ALTER TABLE traffic_logs ADD COLUMN cost_usd REAL` (default NULL)
  - `ALTER TABLE api_keys ADD COLUMN cost_visibility TEXT` (default NULL)
  - Settings keys inserted with defaults: `price_list_url`, `price_list_synced_at`, `display_currency`, `fx_rate_manual`, `fx_rate_auto`, `fx_rate_synced_at`, `cost_visible_to_keys`.
- `pricing.Manager`:
  - Wraps DB connection and `Resolver`.
  - Initializes database with embedded snapshot if `model_prices` is empty.
  - Loads DB prices and overrides into memory `Resolver`.
  - CRUD for price overrides (`SetOverride`, `DeleteOverride`, `ListPrices`).
  - Refresh logic: `RefreshPrices(ctx context.Context)` and `RefreshFX(ctx context.Context)`.
  - Cost settings getter and setter: `GetCostSettings()`, `UpdateCostSettings()`.
- `keys.Manager`:
  - Support `CostVisibility *string` in `KeyInfo`, `CreateKeyOptions`, `UpdateKeyOptions`, and cache loading.

- [x] **Step 1: Write failing migration and manager tests**
  In `internal/db/cost_migration_test.go`: verify migration creates tables, columns, and settings idempotently.
  In `internal/keys/keys_test.go`: verify creating/updating key with `cost_visibility` persists and round-trips.
  In `internal/pricing/manager_test.go`: verify seeding from snapshot on empty DB, adding/deleting override, and settings retrieval.
  Run `go test ./internal/db ./internal/keys ./internal/pricing` -> FAIL.

- [x] **Step 2: Implement schema migration, keys update, and pricing.Manager**
  Update `internal/db/db.go`, `internal/keys/keys.go`, and implement `internal/pricing/manager.go`.

- [x] **Step 3: Run migration and manager tests**
  `CGO_ENABLED=0 go test -tags server ./internal/db ./internal/keys ./internal/pricing` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/db/ internal/keys/ internal/pricing/`
  `git commit -m "feat(pricing): database migrations, cost settings, and pricing manager"`

---

### Task 5: Proxy Write-Time Pricing and Re-Price Engine

**Files:**
- Modify: `internal/traffic/traffic.go`
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/proxy_test.go`
- Create: `internal/pricing/reprice.go`
- Create: `internal/pricing/reprice_test.go`

**Produces:**
- `traffic.LogEntry.CostUSD *float64`:
  - Persisted in `traffic.Record()` as `cost_usd` column.
  - Selected in `GetTraffic()` and `GetTrafficEntry()`.
- Proxy integration in `internal/proxy/proxy.go`:
  - Setter `SetPricingResolver(r *pricing.Resolver)`.
  - On request completion: if not `UsageEstimated`, `TotalTokens > 0`, and `Model != "nineguard/system"`, calculate `costUSD` using resolved `modelID` and prompt/completion/cache tokens.
  - If no price found or condition unmet, `CostUSD` remains `nil` (persisting SQL `NULL`).
- Re-price Engine in `internal/pricing/reprice.go`:
  - `RepriceRange(ctx context.Context, from, to time.Time, modelID string) (updatedCount int, err error)`.
  - Selects matching traffic rows where `usage_estimated = 0 AND status_code < 400 AND model != 'nineguard/system'`.
  - Reads all rows into memory, closes `rows` (adhering to SQLite single connection constraint).
  - Recalculates cost with current Resolver.
  - Updates rows within transaction `tx`.
  - Emits syslog INFO record: range, model, and updated count.

- [x] **Step 1: Write failing tests for proxy write-time pricing and re-price engine**
  In `internal/proxy/proxy_test.go`:
  - Test request completion computes and sets `CostUSD` on `LogEntry`.
  - Test estimated usage request keeps `CostUSD = nil`.
  - Test missing price keeps `CostUSD = nil`.
  In `internal/pricing/reprice_test.go`:
  - Test re-pricing rows updates historical `cost_usd`.
  - Test estimated usage rows are skipped.
  Run `go test ./internal/proxy ./internal/pricing` -> FAIL.

- [x] **Step 2: Implement write-time pricing and reprice engine**
  Update `internal/traffic/traffic.go`, `internal/proxy/proxy.go`, and implement `internal/pricing/reprice.go`.

- [x] **Step 3: Run proxy and reprice tests**
  `CGO_ENABLED=0 go test -tags server ./internal/proxy ./internal/pricing ./internal/traffic` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/traffic/ internal/proxy/ internal/pricing/`
  `git commit -m "feat(pricing): write-time request cost logging and historical reprice engine"`

---

### Task 6: Traffic Aggregates and Reports Cost Integration

**Files:**
- Modify: `internal/traffic/traffic.go`
- Modify: `internal/traffic/traffic_test.go`

**Produces:**
- `DashboardStats`:
  - `EstimatedCostUSD float64` (`SUM(cost_usd)` for the period).
  - `UnpricedRequests int64` (`COUNT(*)` for successful requests with NULL `cost_usd`).
  - `PrevEstimatedCostUSD float64` (`SUM(cost_usd)` for previous period).
  - `PrevUnpricedRequests int64`.
- `UsageReport`:
  - Overall `EstimatedCostUSD float64`, `UnpricedRequests int64`.
  - `KeyUsageBreakdown`: `EstimatedCostUSD float64`, `UnpricedRequests int64`.
  - `ModelUsageBreakdown`: `EstimatedCostUSD float64`, `UnpricedRequests int64`.
- Traffic list queries:
  - Populate `CostUSD *float64` in `LogEntry` items returned by `GetTraffic()`.

- [x] **Step 1: Write failing tests for traffic stats and reports cost aggregation**
  In `internal/traffic/traffic_test.go`:
  - Insert rows with priced and unpriced costs.
  - Verify `GetDashboardStats` calculates `EstimatedCostUSD` and `UnpricedRequests` for current and previous period.
  - Verify `GetUsageReports` aggregates cost per key and per model.
  - Verify `GetTraffic` returns `cost_usd`.
  Run `go test ./internal/traffic` -> FAIL.

- [x] **Step 2: Update SQL queries and mappings in `internal/traffic/traffic.go`**
  Add `COALESCE(SUM(cost_usd), 0)` and `COALESCE(SUM(CASE WHEN cost_usd IS NULL AND status_code < 400 THEN 1 ELSE 0 END), 0)` to queries.

- [x] **Step 3: Run traffic tests**
  `CGO_ENABLED=0 go test -tags server ./internal/traffic` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/traffic/`
  `git commit -m "feat(traffic): cost aggregation for dashboard stats and usage reports"`

---

### Task 7: Pricing & Cost HTTP Handlers and Background Sync

**Files:**
- Create: `internal/handler/prices.go`
- Create: `internal/handler/prices_test.go`
- Modify: `internal/handler/handler.go`
- Modify: `cmd/nineguard/main.go`

**Produces:**
- Handlers in `internal/handler/prices.go`:
  - `GET /api/v1/prices`: returns all known models with effective price, source (`override`/`list`/`none`), family.
  - `PUT /api/v1/prices/overrides/{model_id}`: set override rates (input/output required, cache optional).
  - `DELETE /api/v1/prices/overrides/{model_id}`: delete override.
  - `POST /api/v1/prices/refresh`: trigger immediate price list refresh.
  - `GET /api/v1/settings/cost`: get display currency, rates, sync timestamps.
  - `POST /api/v1/settings/cost`: update display currency, manual FX, cost visibility setting.
  - `POST /api/v1/costs/reprice`: re-price traffic rows in given date/model range.
- Key endpoints update:
  - `POST /api/v1/keys` and `PUT /api/v1/keys/{id}` accept `cost_visibility` (`show`, `hide`, or null).
- Background auto-sync in `cmd/nineguard/main.go`:
  - Hook into the existing 30-minute auto-sync loop to check and run `SyncPricesIfNeeded` and `SyncFXIfNeeded` (at most once every 24h).

- [x] **Step 1: Write failing handler tests**
  In `internal/handler/prices_test.go`:
  - Test `GET /api/v1/prices`.
  - Test `PUT /api/v1/prices/overrides/{model_id}` and `DELETE`.
  - Test `GET /api/v1/settings/cost` and `POST /api/v1/settings/cost`.
  - Test `POST /api/v1/costs/reprice`.
  Run `go test ./internal/handler` -> FAIL.

- [x] **Step 2: Implement handlers and route registration**
  Implement `internal/handler/prices.go`, wire into `internal/handler/handler.go` and `cmd/nineguard/main.go`.

- [x] **Step 3: Run handler tests and server build**
  `CGO_ENABLED=0 go test -tags server ./internal/handler` -> PASS.
  `CGO_ENABLED=0 go build -tags server ./...` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/handler/ cmd/nineguard/`
  `git commit -m "feat(api): cost and pricing endpoints with background sync integration"`

---

### Task 8: Frontend UI — Cost Badges, Currency, Models Table, and Settings

**Files:**
- Create: `web/static/js/costhelpers.js`
- Create: `web/jstest/costhelpers.test.mjs`
- Modify: `web/static/js/views/models.js`
- Modify: `web/static/js/views/traffic.js`
- Modify: `web/static/js/views/dashboard.js`
- Modify: `web/static/js/views/reports.js`
- Modify: `web/static/js/views/endpoints.js`

**Produces:**
- Helper `web/static/js/costhelpers.js`:
  - `formatCost(costUSD, currency, fxRate, unpricedCount)` -> formatted string (e.g. `Rp 2.225.900` or `$12.34`, `≥` prefix if unpriced).
  - `costTooltip(costUSD, currency, fxRate, syncDate)` -> tooltip with list price and FX explanation.
- Models view (`web/static/js/views/models.js`):
  - "Price" column between Tokens and Last Used.
  - Price chip (`$3.00 / $15.00 per 1M`, tooltip with cache rates and source).
  - Source tag: `list`, `override`, `no price`.
  - Click to open edit override dialog (input/output per 1M, optional cache read/write).
- Traffic Explorer (`web/static/js/views/traffic.js`):
  - "Est. Cost" column displaying formatted cost or `—` for NULL.
  - Traffic settings modal: Display Currency select, manual rate input, rate status line, cost visibility toggle, price list status + Refresh button, Re-price dialog button.
  - Re-price modal: Date range inputs, optional model select, confirmation showing warning about pre-Phase 1 uncached pricing.
- Dashboard (`web/static/js/views/dashboard.js`):
  - KPI card "Est. Cost" with delta comparison to previous period.
  - Top keys table gains Est. Cost column.
- Reports (`web/static/js/views/reports.js`):
  - Keys breakdown table: Est. Cost column.
  - Models breakdown table: Est. Cost column.
- Endpoints (`web/static/js/views/endpoints.js`):
  - Key create/edit modal: Cost visibility select (`Follow global setting`, `Always show to key holder`, `Always hide from key holder`).

- [x] **Step 1: Write failing JS tests for cost formatting**
  In `web/jstest/costhelpers.test.mjs`:
  - Format USD standard, format IDR zero decimals, format sub-cent values (`<$0.01`).
  - Format unpriced request count (`≥ ` prefix).
  - Format tooltip text explaining conversion and ECB rate.
  Run `node --test web/jstest/*.mjs` -> FAIL.

- [x] **Step 2: Implement `web/static/js/costhelpers.js`**
  Implement formatters and export functions.

- [x] **Step 3: Run JS tests**
  `node --test web/jstest/*.mjs` -> PASS.

- [x] **Step 4: Update UI views**
  Update `models.js`, `traffic.js`, `dashboard.js`, `reports.js`, `endpoints.js`.

- [x] **Step 5: Verify JS syntax & tests**
  `node --check web/static/js/*.js web/static/js/views/*.js`
  `node --test web/jstest/*.mjs`

- [x] **Step 6: Commit**
  `git add web/`
  `git commit -m "feat(ui): cost visualization across models, traffic, dashboard, and reports"`

---

### Task 9: Architecture Documentation and Full Release Verification

**Files:**
- Modify: `docs/ARCHITECTURE.md` (Indonesian, keep Indonesian)
- Modify: `AGENTS.md`

- [x] **Step 1: Update `docs/ARCHITECTURE.md`**
  Add §3.3 Estimasi Biaya (Estimated Cost):
  - Explain write-time pricing in USD stored in `traffic_logs.cost_usd`.
  - Explain LiteLLM sync and override resolution.
  - Explain multi-currency conversion with Frankfurter / manual rate.
  - Explain Re-price mechanism.

- [x] **Step 2: Update `AGENTS.md`**
  Add notes regarding write-time cost immutability (ADR 0012) and display currency conversion rules.

- [x] **Step 3: Run all quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`
  4. `node --check web/static/js/*.js web/static/js/views/*.js`

- [x] **Step 4: Commit**
  `git add docs/ARCHITECTURE.md AGENTS.md docs/superpowers/plans/2026-10-19-estimated-cost.md`
  `git commit -m "docs: architecture and developer rules for estimated cost"`