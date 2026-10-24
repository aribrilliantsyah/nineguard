# Implementation Plan: Phase 3 — Usage Summary

- **Goal:** Implement key-level Usage Summary reporting across `/usage` chat command, `GET /v1/usage`, admin endpoint `GET /api/v1/keys/{id}/usage`, upstream cascaded usage, usage comparison against books, and admin UI panel with daily heatmap and stacked model charts.
- **Spec Reference:** `docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md` §6.
- **ADR Reference:** `docs/adr/0013-upstream-usage-aggregate-only.md`.
- **Target Branch:** `feat/usage-summary` (branched from `feat/estimated-cost`).

---

## Global Constraints & Principles

1. **Range Definition:** `7d`, `30d` = last N local calendar days including today; `all` = since first row (daily heatmap capped at last 371 days / 53 weeks).
2. **Timezone Anchor:** `/usage` and `GET /v1/usage` use the key's Effective Reset Schedule timezone (UTC when default). Admin panel uses viewer's `?tz=` query parameter.
3. **Visibility & Privacy:** When `ShowCost(key)` is false, `cost` and `upstream_usage` are omitted entirely from `/usage` and `GET /v1/usage`. Admin endpoints always include cost.
4. **Cascaded Upstream Usage (ADR 0013):** For active providers with `quota_type = nineguard`, fetch `GET <base>/v1/usage?range=...` using the provider key. Cached for 5 minutes. Never merged into user numbers; presented strictly as aggregate upstream capacity.
5. **SQLite Single Connection:** Single open connection (`SetMaxOpenConns(1)`). Read, close `rows`, then proceed.
6. **Command Separation:** `/usage [7d|30d|all]` serves Usage Summary; `/quota [filter]` and `/limits` serve Quota Report.
7. **Quality Gates:**
   - `CGO_ENABLED=0 go test -tags server ./...`
   - `CGO_ENABLED=0 go build -tags server ./...`
   - `node --test web/jstest/*.mjs`
   - `node --check web/static/js/*.js web/static/js/views/*.js`

---

## Tasks

### Task 1: Books Token Comparison and Inferred Sessions Calculation

**Files:**
- Create: `internal/traffic/books.go`
- Create: `internal/traffic/books_test.go`
- Modify: `internal/traffic/traffic.go`
- Create: `internal/traffic/summary_types.go`

**Produces:**
- Books table in `internal/traffic/books.go`:
  - `The Little Prince`: 17,000 words (22,666 tokens)
  - `Animal Farm`: 30,000 words (40,000 tokens)
  - `The Hobbit`: 95,000 words (126,666 tokens)
  - `Harry Potter and the Philosopher's Stone`: 77,000 words (102,666 tokens)
  - `Pride and Prejudice`: 122,000 words (162,666 tokens)
  - `Moby-Dick`: 206,000 words (274,666 tokens)
  - `The Lord of the Rings`: 481,000 words (641,333 tokens)
  - `War and Peace`: 587,000 words (782,666 tokens)
  - `The Bible (KJV)`: 783,000 words (1,044,000 tokens)
  - `English Wikipedia`: 4,500,000,000 words (6,000,000,000 tokens)
- Pure function: `CompareTokens(totalTokens int64) *ComparisonResult`:
  - If totalTokens <= 0 -> returns nil.
  - Finds the largest book with `book_tokens <= totalTokens`.
  - Multiple = `float64(totalTokens) / float64(book_tokens)`.
  - If below smallest book: calculates fraction (`~40% of The Little Prince`).
  - Human string format: `You've used ~117,869× more tokens than Animal Farm.`
- Summary data structures in `summary_types.go`:
  - `UsageSummary`, `DailyUsagePoint`, `ModelUsagePoint`, `TokensBreakdown`, `ThinkingBreakdown`, `CostBreakdown`, `ComparisonResult`, `UpstreamUsageEntry`.

- [x] **Step 1: Write failing tests in `internal/traffic/books_test.go`**
  - Test comparison below smallest book (fraction).
  - Test comparison exactly equal to book.
  - Test comparison between books (picks the largest smaller book).
  - Test comparison above English Wikipedia.
  - Test zero and negative tokens return nil.
  Run `go test ./internal/traffic` -> FAIL.

- [x] **Step 2: Implement `internal/traffic/books.go` and `summary_types.go`**
  Implement books list and `CompareTokens` matching Spec §6.5.

- [x] **Step 3: Run unit tests**
  `CGO_ENABLED=0 go test -tags server -run TestCompareTokens ./internal/traffic` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/traffic/`
  `git commit -m "feat(traffic): books token comparison and usage summary structures"`

---

### Task 2: Usage Summary Computation Engine

**Files:**
- Create: `internal/traffic/summary.go`
- Create: `internal/traffic/summary_test.go`
- Modify: `internal/traffic/quota.go`

**Produces:**
- Settings:
  - `session_gap_minutes` (default `"30"`, integer >= 1)
  - `usage_comparison_enabled` (default `"true"`, boolean)
  - Getters/setters on `traffic.Manager`.
- Engine: `GetUsageSummary(database *db.DB, pricingMgr *pricing.Manager, keyID string, rng string, loc *time.Location, now time.Time, viewerTz string) (*UsageSummary, error)`:
  - Range bounds: `7d`, `30d`, `all` computed in local timezone `loc`.
  - Excludes `model = 'nineguard/system'`.
  - Requests (status < 400), Errors (status >= 400).
  - Tokens: prompt, completion, total, cache_read, cache_write, reasoning (over status < 400).
  - Cache hit % = cache_read / prompt.
  - Sessions: `LAG() OVER (ORDER BY timestamp ASC)` with gap > `session_gap_minutes * 60` seconds.
  - Active days: distinct local calendar dates.
  - Peak hour: local hour with most requests.
  - Favorite model: model with highest total tokens.
  - Thinking: requested, delivered, lost counts.
  - Fallbacks: fallback request count.
  - Cost: USD sum, display currency conversion using current effective rate, unpriced requests count.
  - Daily: array of local date points (capped at 371 days for `all`).
  - Models: top 5 models + "other", with daily sub-series.
  - Comparison: book multiple.

- [x] **Step 1: Write failing tests in `internal/traffic/summary_test.go`**
  - Test session calculation: 0 rows -> 0; 1 row -> 1; 2 rows within 20m -> 1 session; 2 rows 45m apart -> 2 sessions.
  - Test peak hour tie-breaker (picks earliest hour).
  - Test favorite model selected by tokens, not requests.
  - Test timezones: Asia/Jakarta vs America/New_York (DST boundary).
  - Test unpriced request counting and cost calculation.
  - Test 7d, 30d, all ranges.
  Run `go test ./internal/traffic` -> FAIL.

- [x] **Step 2: Implement `internal/traffic/summary.go`**
  Implement `GetUsageSummary` and session gap settings.

- [x] **Step 3: Run summary tests**
  `CGO_ENABLED=0 go test -tags server -run TestGetUsageSummary ./internal/traffic` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/traffic/`
  `git commit -m "feat(traffic): usage summary engine with sessions and model breakdown"`

---

### Task 3: Upstream Gateway Cascaded Usage Fetcher

**Files:**
- Create: `internal/upstream/usage.go`
- Create: `internal/upstream/usage_test.go`
- Modify: `internal/upstream/types.go`

**Produces:**
- Method: `FetchUpstreamUsage(ctx context.Context, providerID, baseURL, apiKey, rng string) (*traffic.UsageSummary, error)`:
  - Sends `GET <base>/v1/usage?range=<rng>` with `Authorization: Bearer <apiKey>` and loop-prevention hop headers.
  - 5-second timeout.
  - In-memory cache with 5-minute TTL per `(providerID, rng)`.
  - Handles 404 (older NineGuard gateway) by returning a muted error.

- [x] **Step 1: Write failing tests in `internal/upstream/usage_test.go`**
  - Test mock upstream server returning 200 with summary JSON.
  - Test mock upstream server returning 404 (older gateway).
  - Test 5-minute caching avoids duplicate HTTP calls.
  Run `go test ./internal/upstream` -> FAIL.

- [x] **Step 2: Implement `internal/upstream/usage.go`**
  Implement HTTP client and cache.

- [x] **Step 3: Run upstream tests**
  `CGO_ENABLED=0 go test -tags server ./internal/upstream` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/upstream/`
  `git commit -m "feat(upstream): cascaded usage summary fetcher with 5-minute cache"`

---

### Task 4: Proxy Chat Command `/usage` and `GET /v1/usage` Interception

**Files:**
- Modify: `internal/proxy/chat_command.go`
- Create: `internal/proxy/usage_command.go`
- Create: `internal/proxy/usage_command_test.go`
- Modify: `internal/proxy/proxy.go`

**Produces:**
- `FormatUsageMarkdown(summary *traffic.UsageSummary, arg string, showCost bool) string`:
  - Renders markdown matching Spec §6.6 example.
  - Shows requests, error rate %, sessions (inferred), active days.
  - Tokens with cache hit % and estimated cost (if showCost).
  - Peak hour, favorite model, thinking stats, fallbacks.
  - Markdown table of models with In, Out, Cost, Share.
  - Book comparison line.
  - Cascaded upstream usage summary line.
- Chat command interception:
  - In `proxy.go`: when command is `/usage`, route to `FormatUsageMarkdown` instead of quota markdown.
  - Accepts `7d`, `30d`, `all`. Unrecognized args show `Unknown range "...", showing 30d. Use /usage 7d|30d|all.`
- Endpoint `GET /v1/usage`:
  - Intercepted beside `/v1/quota`. Authenticated by client key.
  - Range parameter: `7d`, `30d`, `all` (default `30d`, invalid -> 400).
  - Evaluates `ShowCost(key)`: if false, omits `cost` and `upstream_usage`.
  - Evaluates `usage_comparison_enabled`: if false, omits `comparison`.

- [x] **Step 1: Write failing tests for `/usage` formatting and proxy interception**
  In `internal/proxy/usage_command_test.go`:
  - Test `FormatUsageMarkdown` with cost visible and hidden.
  - Test invalid range parameter hint.
  - Test `GET /v1/usage` HTTP handler with 200, bad range 400, unauthorized 401.
  Run `go test ./internal/proxy` -> FAIL.

- [x] **Step 2: Implement `/usage` formatter and proxy handler**
  Update `chat_command.go`, `usage_command.go`, and `proxy.go`.

- [x] **Step 3: Run proxy tests**
  `CGO_ENABLED=0 go test -tags server ./internal/proxy` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/proxy/`
  `git commit -m "feat(proxy): /usage chat command and GET /v1/usage endpoint"`

---

### Task 5: Admin Usage API Endpoint

**Files:**
- Modify: `internal/handler/handler.go`
- Create: `internal/handler/key_usage_test.go`
- Modify: `cmd/nineguard/main.go`

**Produces:**
- Handler `GetKeyUsage(w http.ResponseWriter, r *http.Request)`:
  - Route: `GET /api/v1/keys/{id}/usage?range=7d|30d|all&tz=...`
  - Validates key exists.
  - Parses `range` (default `30d`) and `tz` (default UTC or key's schedule).
  - Calls `traffic.GetUsageSummary(...)`.
  - Always includes cost (admin view). Excludes `upstream_usage` (admins see upstream on quotas page).
- Route registration in `cmd/nineguard/main.go`.

- [x] **Step 1: Write failing handler tests in `internal/handler/key_usage_test.go`**
  - Test `GET /api/v1/keys/{id}/usage` returns 200 with full summary object.
  - Test invalid key ID returns 404.
  - Test invalid range returns 400.
  Run `go test ./internal/handler` -> FAIL.

- [x] **Step 2: Implement `GetKeyUsage` and register route**
  Implement handler in `internal/handler/handler.go` and wire in `cmd/nineguard/main.go`.

- [x] **Step 3: Run handler tests**
  `CGO_ENABLED=0 go test -tags server -run TestGetKeyUsage ./internal/handler` -> PASS.

- [x] **Step 4: Commit**
  `git add internal/handler/ cmd/nineguard/`
  `git commit -m "feat(api): admin key usage summary endpoint"`

---

### Task 6: Frontend UI — Admin Key Usage View (KPIs, Heatmap, and Models)

**Files:**
- Create: `web/static/js/views/keyusage.js`
- Create: `web/jstest/keyusage.test.mjs`
- Modify: `web/static/js/views/endpoints.js`
- Modify: `web/static/js/router.js` (if needed for key usage route or drawer)

**Produces:**
- `keyusage.js`:
  - Range selector pills: `7d`, `30d`, `All`.
  - Tabs: **Overview** and **Models**.
  - Overview tab:
    - KPI grid: Requests, Sessions (inferred), Total Tokens, Est. Cost, Active Days, Peak Hour, Favorite Model, Cache Hit %, Thinking Lost, Fallbacks.
    - Daily token heatmap: columns as weeks, rows as weekdays (Mon-Sun), 5 intensity color levels based on token volume.
    - Comparison line: `You've used ~X more tokens than Animal Farm.`
  - Models tab:
    - Stacked daily token bar chart per model.
    - Table: Model, Input Tokens, Output Tokens, Est. Cost, System Share %.
- Endpoints view (`endpoints.js`):
  - Add "Usage" button on each key row to open the Key Usage modal or navigate to key usage view.

- [x] **Step 1: Write failing JS tests for heatmap and range helper**
  In `web/jstest/keyusage.test.mjs`:
  - Test heatmap cell color intensity buckets (0 to 4).
  - Test range parameter mapping.
  Run `node --test web/jstest/*.mjs` -> FAIL.

- [x] **Step 2: Implement `web/static/js/views/keyusage.js`**
  Implement view matching the reference designs.

- [x] **Step 3: Run JS tests**
  `node --test web/jstest/*.mjs` -> PASS.

- [x] **Step 4: Update `web/static/js/views/endpoints.js`**
  Add "Usage" action button opening `keyusage.js` modal.

- [x] **Step 5: Verify JS syntax & quality gates**
  `node --check web/static/js/views/keyusage.js`
  `node --check web/static/js/views/endpoints.js`
  `node --test web/jstest/*.mjs`

- [x] **Step 6: Commit**
  `git add web/`
  `git commit -m "feat(ui): key usage summary view with heatmap, KPIs, and model breakdowns"`

---

### Task 7: Architecture Documentation and Full Release Verification

**Files:**
- Modify: `docs/ARCHITECTURE.md` (Indonesian, keep Indonesian)
- Modify: `AGENTS.md`
- Modify: `README.md` (mention `/usage` and `GET /v1/usage`)

- [x] **Step 1: Update `docs/ARCHITECTURE.md`**
  Add §3.4 Ringkasan Penggunaan (Usage Summary):
  - Document `/usage` chat command and `GET /v1/usage`.
  - Document inferred session logic (`session_gap_minutes`).
  - Document cascaded upstream usage aggregation (ADR 0013).
  - Document token comparison against literature.

- [x] **Step 2: Update `AGENTS.md` and `README.md`**
  Document the `/usage` command behavior change vs `/quota`.

- [x] **Step 3: Run all quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`
  4. `node --check web/static/js/*.js web/static/js/views/*.js`

- [x] **Step 4: Commit**
  `git add docs/ AGENTS.md README.md docs/superpowers/plans/2026-10-19-usage-summary.md`
  `git commit -m "docs: architecture and release notes for usage summary"`