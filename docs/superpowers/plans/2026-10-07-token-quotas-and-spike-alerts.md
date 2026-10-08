# Token Quotas & Spike Alerts — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Enforce daily, weekly, monthly, and lifetime token quotas per client API key via soft post-facto gating with UTC windows and standard HTTP 429 responses, plus flag anomalous heavy requests (spikes) in traffic logs and system telemetry without blocking requests.

**Architecture:**
- **Schema & Model:** `api_keys` table gains `quota_limit` (integer, 0 = unlimited) and `quota_period` (`none`, `daily`, `weekly`, `monthly`, `total`). `settings` table seeds default `heavy_token_threshold = 8000`.
- **Quota Engine (`internal/traffic/quota.go`):** Computes consumed tokens within the active UTC window using the existing SQLite index `idx_traffic_key_id_ts(api_key_id, timestamp)`. Calculates deterministic UTC boundary starts and reset times (`Retry-After`).
- **Proxy Hot Path (`internal/proxy/proxy.go`):** Pre-flight check queries `GetQuotaUsage` if key has a non-zero quota. If `consumed >= limit`, returns HTTP 429 with `Retry-After` header and informative JSON. Admitted requests run to completion. Post-flight check flags requests exceeding `heavy_token_threshold` and emits a `WARN` syslog entry.
- **Management API (`internal/handler/handler.go`):** Validates quota fields on key create/update; serves settings for heavy token threshold.
- **Web UI (`web/static/js/`):** Key modal inputs for quota configuration, table progress bar with quota exhaustion alerts, relative reset countdown in viewer local timezone, and Traffic Explorer heavy badges.

**Tech Stack:** Go 1.27.1, `modernc.org/sqlite` (pure Go), `net/http`, vanilla ES modules, `node --test`.

**Specs & Decisions:**
- Spec: `docs/FUTURE_PLANNED_FEATURES.md` (§2 and §3)
- ADR: `docs/adr/0005-token-quotas-utc-windows.md`
- Glossary: `GLOSSARY.md` (`Token Quota`, `Quota Period`, `Soft Post-Facto Enforcement`, `Heavy Request`)

---

## Global Constraints

- Go `1.27.1`; all builds and tests must pass `CGO_ENABLED=0 go test -tags server ./...`. No new external Go dependencies.
- SQLite query on quota must run against existing composite index `idx_traffic_key_id_ts(api_key_id, timestamp)` without table scanning.
- Quota windows strictly anchor to UTC calendar boundaries:
  - `daily`: today 00:00:00 UTC to tomorrow 00:00:00 UTC.
  - `weekly`: Monday 00:00:00 UTC (ISO 8601) to next Monday 00:00:00 UTC.
  - `monthly`: 1st of month 00:00:00 UTC to 1st of next month 00:00:00 UTC.
  - `total`: all-time (`timestamp >= '1970-01-01 00:00:00'`).
- HTTP 429 response conforms to OpenAI client expectations:
  - Header: `Retry-After: <seconds_until_utc_reset>`
  - Body:
    ```json
    {
      "error": {
        "message": "API key token quota exceeded (124,500 / 100,000 tokens daily). Resets in 3h 42m (at 2026-10-08T00:00:00Z).",
        "type": "insufficient_quota",
        "code": "quota_exceeded"
      }
    }
    ```
- JS unit tests must live in `web/jstest/` and run under `node --test`.
- Atomic commits per task.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/db/db.go` | Schema migrations for `api_keys` quota columns; seed `heavy_token_threshold` |
| `internal/db/quota_migration_test.go` (new) | Unit test verifying column presence, defaults, and setting seed |
| `internal/keys/keys.go` | `KeyInfo` struct fields, SQL queries (`GetKey`, `ListKeys`, `CreateKey`, `UpdateKey`) |
| `internal/keys/page.go` | Paged key queries including quota columns |
| `internal/keys/quota_test.go` (new) | Key CRUD test with quota fields |
| `internal/traffic/quota.go` (new) | Pure UTC window calculations and `GetQuotaUsage` query |
| `internal/traffic/quota_test.go` (new) | Unit tests for calendar boundaries, reset timestamps, and SQL queries |
| `internal/proxy/proxy.go` | Pre-flight quota gating (429 + Retry-After); post-flight heavy token spike alert |
| `internal/proxy/quota_proxy_test.go` (new) | Integration test for quota gating and spike logging |
| `internal/handler/handler.go` | Request validation for key quota limits/periods and settings |
| `internal/handler/quota_handler_test.go` (new) | Handler validation tests for quota payloads |
| `web/static/js/quotahelpers.js` (new) | Pure frontend formatting functions for quota bars, countdowns, and reset text |
| `web/jstest/quotahelpers.test.mjs` (new) | `node --test` suite for quota helpers |
| `web/static/js/views/endpoints.js` | UI inputs in Key modal and quota progress bar in table |
| `web/static/js/views/traffic.js` | ⚠️ `Heavy` badge in Traffic Explorer and filter option |
| `web/static/js/views/settings.js` | UI input for `heavy_token_threshold` |

---

# Tasks

### Task 1: Database Migration for Quotas & Spike Setting

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/quota_migration_test.go`

- [x] **Step 1: Write the failing test**
  Create `internal/db/quota_migration_test.go`:
  ```go
  package db_test

  import (
  	"testing"
  	"nineguard/internal/db"
  )

  func TestQuotaColumnsAndSettingsSeeded(t *testing.T) {
  	d, err := db.InitDB(":memory:")
  	if err != nil {
  		t.Fatalf("InitDB: %v", err)
  	}
  	defer d.Close()

  	// Verify columns exist on api_keys
  	var quotaLimit int64
  	var quotaPeriod string
  	err = d.QueryRow("SELECT quota_limit, quota_period FROM api_keys LIMIT 0").Scan(&quotaLimit, &quotaPeriod)
  	if err != nil {
  		t.Errorf("api_keys quota columns missing: %v", err)
  	}

  	// Verify heavy_token_threshold setting seeded
  	var val string
  	err = d.QueryRow("SELECT value FROM settings WHERE key = 'heavy_token_threshold'").Scan(&val)
  	if err != nil || val != "8000" {
  		t.Errorf("expected heavy_token_threshold '8000', got %q, err: %v", val, err)
  	}
  }
  ```
- [x] **Step 2: Run test to verify failure**
  Run: `go test ./internal/db/ -run TestQuotaColumnsAndSettingsSeeded -v`
  Expected: FAIL (columns missing, setting missing).
- [x] **Step 3: Implement database migrations**
  In `internal/db/db.go`:
  - In `CREATE TABLE IF NOT EXISTS api_keys`, add `quota_limit INTEGER DEFAULT 0` and `quota_period TEXT DEFAULT 'none'`.
  - In migration section for existing databases:
    ```go
    _, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_limit INTEGER DEFAULT 0")
    _, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_period TEXT DEFAULT 'none'")
    _, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('heavy_token_threshold', '8000')")
    ```
- [x] **Step 4: Run test to verify pass**
  Run: `go test ./internal/db/ -v`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add internal/db/db.go internal/db/quota_migration_test.go
  git commit -m "feat(db): add quota columns to api_keys and seed heavy_token_threshold"
  ```

---

### Task 2: Update `internal/keys` Structs & CRUD Queries

**Files:**
- Modify: `internal/keys/keys.go`
- Modify: `internal/keys/page.go`
- Create: `internal/keys/quota_test.go`

- [x] **Step 1: Write the failing test**
  Create `internal/keys/quota_test.go`:
  ```go
  package keys_test

  import (
  	"testing"
  	"nineguard/internal/db"
  	"nineguard/internal/keys"
  )

  func TestKeyQuotaCRUD(t *testing.T) {
  	d, err := db.InitDB(":memory:")
  	if err != nil {
  		t.Fatalf("InitDB: %v", err)
  	}
  	defer d.Close()

  	mgr := keys.NewManager(d)

  	// Create key with quota
  	info, err := mgr.CreateKeyWithOptions("test-quota", keys.CreateKeyOptions{
  		QuotaLimit:  50000,
  		QuotaPeriod: "daily",
  	})
  	if err != nil {
  		t.Fatalf("CreateKey: %v", err)
  	}
  	if info.QuotaLimit != 50000 || info.QuotaPeriod != "daily" {
  		t.Errorf("created key mismatch: %+v", info)
  	}

  	// Get key
  	got, err := mgr.GetKey(info.ID)
  	if err != nil {
  		t.Fatalf("GetKey: %v", err)
  	}
  	if got.QuotaLimit != 50000 || got.QuotaPeriod != "daily" {
  		t.Errorf("got key mismatch: %+v", got)
  	}

  	// Update quota
  	err = mgr.UpdateKeyQuota(info.ID, 100000, "monthly")
  	if err != nil {
  		t.Fatalf("UpdateKeyQuota: %v", err)
  	}

  	gotAfter, err := mgr.GetKey(info.ID)
  	if err != nil {
  		t.Fatalf("GetKey: %v", err)
  	}
  	if gotAfter.QuotaLimit != 100000 || gotAfter.QuotaPeriod != "monthly" {
  		t.Errorf("updated key mismatch: %+v", gotAfter)
  	}
  }
  ```
- [x] **Step 2: Run test to verify failure**
  Run: `go test ./internal/keys/ -run TestKeyQuotaCRUD -v`
  Expected: FAIL (compilation errors on missing fields/methods).
- [x] **Step 3: Update `KeyInfo`, `CreateKeyOptions`, and SQL queries**
  In `internal/keys/keys.go`:
  - Add `QuotaLimit int64` (`json:"quota_limit"`) and `QuotaPeriod string` (`json:"quota_period"`) to `KeyInfo`.
  - Add `QuotaLimit int64` and `QuotaPeriod string` to `CreateKeyOptions`.
  - Update `SELECT ... quota_limit, quota_period` in `GetKey`, `ListKeys`, and `scanKey`.
  - Add `UpdateKeyQuota(id string, limit int64, period string) error`.
  In `internal/keys/page.go`:
  - Update `ListKeysPage` SELECT query and column scans to include `quota_limit` and `quota_period`.
- [x] **Step 4: Run test to verify pass**
  Run: `go test ./internal/keys/ -v`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add internal/keys/keys.go internal/keys/page.go internal/keys/quota_test.go
  git commit -m "feat(keys): add quota limit and period fields to KeyInfo and CRUD"
  ```

---

### Task 3: Quota Engine — Boundary Math & Usage Tallying (`internal/traffic`)

**Files:**
- Create: `internal/traffic/quota.go`
- Create: `internal/traffic/quota_test.go`

- [x] **Step 1: Write the failing test**
  Create `internal/traffic/quota_test.go`:
  ```go
  package traffic_test

  import (
  	"testing"
  	"time"
  	"nineguard/internal/db"
  	"nineguard/internal/traffic"
  )

  func TestCalcWindowBoundsUTC(t *testing.T) {
  	// Fixed instant: Wednesday 2026-10-07 14:30:00 UTC
  	now := time.Date(2026, 10, 7, 14, 30, 0, 0, time.UTC)

  	// Daily
  	start, resetAt := traffic.CalcQuotaWindow("daily", now)
  	if !start.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("daily start mismatch: got %v", start)
  	}
  	if !resetAt.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("daily reset mismatch: got %v", resetAt)
  	}

  	// Weekly (Monday 00:00 UTC)
  	startW, resetAtW := traffic.CalcQuotaWindow("weekly", now)
  	if !startW.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("weekly start mismatch: got %v (expected Mon Oct 5)", startW)
  	}
  	if !resetAtW.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("weekly reset mismatch: got %v (expected Mon Oct 12)", resetAtW)
  	}

  	// Monthly (1st of month 00:00 UTC)
  	startM, resetAtM := traffic.CalcQuotaWindow("monthly", now)
  	if !startM.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("monthly start mismatch: got %v", startM)
  	}
  	if !resetAtM.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
  		t.Errorf("monthly reset mismatch: got %v", resetAtM)
  	}
  }

  func TestGetQuotaUsageQuery(t *testing.T) {
  	d, err := db.InitDB(":memory:")
  	if err != nil {
  		t.Fatalf("InitDB: %v", err)
  	}
  	defer d.Close()

  	keyID := "key-test-123"
  	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

  	// Insert traffic row inside window
  	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id, total_tokens, timestamp) VALUES (?, ?, ?)`,
  		keyID, 3500, "2026-10-07 10:00:00")
  	// Insert traffic row outside daily window (yesterday)
  	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id, total_tokens, timestamp) VALUES (?, ?, ?)`,
  		keyID, 5000, "2026-10-06 23:00:00")

  	usage, resetAt, err := traffic.GetQuotaUsage(d, keyID, "daily", now)
  	if err != nil {
  		t.Fatalf("GetQuotaUsage: %v", err)
  	}
  	if usage != 3500 {
  		t.Errorf("expected 3500 tokens in daily window, got %d", usage)
  	}
  	if resetAt.Before(now) {
  		t.Errorf("resetAt should be future, got %v", resetAt)
  	}
  }
  ```
- [x] **Step 2: Run test to verify failure**
  Run: `go test ./internal/traffic/ -run 'TestCalcWindowBoundsUTC|TestGetQuotaUsageQuery' -v`
  Expected: FAIL (symbols undefined).
- [x] **Step 3: Implement `internal/traffic/quota.go`**
  ```go
  package traffic

  import (
  	"database/sql"
  	"fmt"
  	"time"
  	"nineguard/internal/db"
  	"nineguard/internal/timeutil"
  )

  func CalcQuotaWindow(period string, now time.Time) (start time.Time, resetAt time.Time) {
  	now = now.UTC()
  	switch period {
  	case "daily":
  		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
  		resetAt = start.AddDate(0, 0, 1)
  	case "weekly":
  		weekday := int(now.Weekday())
  		if weekday == 0 { // Sunday in Go is 0, ISO Monday is 1
  			weekday = 7
  		}
  		daysSinceMonday := weekday - 1
  		start = time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
  		resetAt = start.AddDate(0, 0, 7)
  	case "monthly":
  		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
  		resetAt = start.AddDate(0, 1, 0)
  	case "total":
  		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
  		resetAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
  	default:
  		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
  		resetAt = now
  	}
  	return start, resetAt
  }

  func GetQuotaUsage(d *db.DB, apiKeyID string, period string, now time.Time) (int64, time.Time, error) {
  	if period == "none" || period == "" {
  		return 0, time.Time{}, nil
  	}
  	start, resetAt := CalcQuotaWindow(period, now)
  	startStr := start.Format(timeutil.SQLiteLayout)

  	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM traffic_logs WHERE api_key_id = ? AND timestamp >= ?`
  	var consumed int64
  	err := d.QueryRow(query, apiKeyID, startStr).Scan(&consumed)
  	if err != nil && err != sql.ErrNoRows {
  		return 0, resetAt, fmt.Errorf("query quota usage: %w", err)
  	}
  	return consumed, resetAt, nil
  }
  ```
- [x] **Step 4: Run test to verify pass**
  Run: `go test ./internal/traffic/ -run 'TestCalcWindowBoundsUTC|TestGetQuotaUsageQuery' -v`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add internal/traffic/quota.go internal/traffic/quota_test.go
  git commit -m "feat(traffic): add quota window calculation and usage query"
  ```

---

### Task 4: Proxy Enforcement (HTTP 429) & Heavy Token Telemetry

**Files:**
- Modify: `internal/proxy/proxy.go`
- Create: `internal/proxy/quota_proxy_test.go`

- [x] **Step 1: Write the failing test**
  Create `internal/proxy/quota_proxy_test.go`:
  ```go
  package proxy_test

  import (
  	"net/http"
  	"net/http/httptest"
  	"strings"
  	"testing"
  	"time"

  	"nineguard/internal/db"
  	"nineguard/internal/keys"
  	"nineguard/internal/proxy"
  )

  func TestProxyQuotaExceededReturns429(t *testing.T) {
  	d, err := db.InitDB(":memory:")
  	if err != nil {
  		t.Fatalf("InitDB: %v", err)
  	}
  	defer d.Close()

  	keyMgr := keys.NewManager(d)
  	k, err := keyMgr.CreateKeyWithOptions("quota-key", keys.CreateKeyOptions{
  		QuotaLimit:  1000,
  		QuotaPeriod: "daily",
  	})
  	if err != nil {
  		t.Fatalf("CreateKey: %v", err)
  	}

  	// Insert past traffic exceeding 1000 tokens today
  	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id, total_tokens, timestamp) VALUES (?, ?, datetime('now'))`,
  		k.ID, 1500)

  	p := proxy.NewProxy(d, keyMgr, nil, nil, nil)

  	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
  	req.Header.Set("Authorization", "Bearer "+k.RawKey)
  	w := httptest.NewRecorder()

  	p.ServeHTTP(w, req)

  	if w.Code != http.StatusTooManyRequests {
  		t.Fatalf("expected status 429, got %d: %s", w.Code, w.Body.String())
  	}
  	if retryAfter := w.Header().Get("Retry-After"); retryAfter == "" {
  		t.Errorf("expected Retry-After header to be set")
  	}
  	if !strings.Contains(w.Body.String(), "quota_exceeded") {
  		t.Errorf("expected quota_exceeded in body, got %s", w.Body.String())
  	}
  }
  ```
- [x] **Step 2: Run test to verify failure**
  Run: `go test ./internal/proxy/ -run TestProxyQuotaExceededReturns429 -v`
  Expected: FAIL.
- [x] **Step 3: Implement pre-flight quota check and post-flight heavy token check**
  In `internal/proxy/proxy.go`:
  - Before forwarding, inspect `keyInfo.QuotaLimit > 0` and `keyInfo.QuotaPeriod != "none"`.
  - Call `traffic.GetQuotaUsage(d, keyInfo.ID, keyInfo.QuotaPeriod, time.Now())`.
  - If `consumed >= keyInfo.QuotaLimit`:
    - Calculate `secs := int(time.Until(resetAt).Seconds())`.
    - Set header `Retry-After: strconv.Itoa(secs)`.
    - Format response JSON per ADR-0005 with human duration and return HTTP 429.
  - On request completion, check `total_tokens >= heavyThreshold` (read from `settings` or cached).
  - If exceeded, emit `slog.Warn("token_spike", "source", "traffic", "key_id", keyInfo.ID, "tokens", totalTokens)`.
- [x] **Step 4: Run test to verify pass**
  Run: `go test ./internal/proxy/ -v`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add internal/proxy/proxy.go internal/proxy/quota_proxy_test.go
  git commit -m "feat(proxy): enforce token quotas with 429 Retry-After and emit spike alerts"
  ```

---

### Task 5: Handler Key Validation & Settings Exposure

**Files:**
- Modify: `internal/handler/handler.go`
- Create: `internal/handler/quota_handler_test.go`

- [x] **Step 1: Write the failing test**
  Create `internal/handler/quota_handler_test.go` testing that `POST /api/v1/keys` rejects invalid `quota_period` (e.g. "yearly") and negative `quota_limit`.
- [x] **Step 2: Run test to verify failure**
  Run: `go test ./internal/handler/ -run TestQuotaValidation -v`
  Expected: FAIL.
- [x] **Step 3: Implement payload validation**
  In `internal/handler/handler.go`:
  - Validate `quota_limit >= 0`.
  - Validate `quota_period` in `["none", "daily", "weekly", "monthly", "total"]`.
  - Pass fields to `keyMgr.CreateKeyWithOptions` and `keyMgr.UpdateKeyQuota`.
  - Ensure `GET /api/v1/settings` and `POST /api/v1/settings` allow updating `heavy_token_threshold`.
- [x] **Step 4: Run test to verify pass**
  Run: `go test ./internal/handler/ -v`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add internal/handler/handler.go internal/handler/quota_handler_test.go
  git commit -m "feat(handler): validate quota fields on key endpoints and expose spike setting"
  ```

---

### Task 6: Frontend Pure Quota Helpers & Node Tests

**Files:**
- Create: `web/static/js/quotahelpers.js`
- Create: `web/jstest/quotahelpers.test.mjs`

- [x] **Step 1: Write the failing test**
  Create `web/jstest/quotahelpers.test.mjs`:
  ```javascript
  import test from 'node:test';
  import assert from 'node:assert/strict';
  import { formatQuotaUsage, quotaPercent, quotaResetCountdown } from '../static/js/quotahelpers.js';

  test('quotaPercent calculates capped percentage', () => {
    assert.equal(quotaPercent(2500, 10000), 25);
    assert.equal(quotaPercent(15000, 10000), 100);
    assert.equal(quotaPercent(0, 0), 0);
  });

  test('formatQuotaUsage formats token counts', () => {
    assert.equal(formatQuotaUsage(25000, 50000, 'daily'), '25k / 50k daily (50%)');
    assert.equal(formatQuotaUsage(0, 0, 'none'), 'Unlimited');
  });
  ```
- [x] **Step 2: Run test to verify failure**
  Run: `node --test web/jstest/quotahelpers.test.mjs`
  Expected: FAIL.
- [x] **Step 3: Implement `web/static/js/quotahelpers.js`**
  Implement `quotaPercent`, `formatQuotaUsage`, and `quotaResetCountdown` (converts UTC reset into viewer local timezone string and relative hours/minutes).
- [x] **Step 4: Run test to verify pass**
  Run: `node --test web/jstest/quotahelpers.test.mjs`
  Expected: PASS.
- [x] **Step 5: Commit**
  ```bash
  git add web/static/js/quotahelpers.js web/jstest/quotahelpers.test.mjs
  git commit -m "feat(ui): add pure quota formatting helpers with unit tests"
  ```

---

### Task 7: Endpoints & Keys UI Integration

**Files:**
- Modify: `web/static/js/views/endpoints.js`

- [x] **Step 1: Update Key Modal**
  Add inputs for:
  - `Quota Limit` (number input, placeholder "0 = unlimited").
  - `Quota Period` (select: `Unlimited (none)`, `Daily (UTC)`, `Weekly (UTC Mon)`, `Monthly (UTC 1st)`, `Lifetime (total)`).
- [x] **Step 2: Update Keys Table Row**
  Display quota progress bar and exhaustion pill when `quota_limit > 0`.
- [x] **Step 3: Smoke test in browser**
  Verify creating a key with daily quota, editing quota, and rendering bar.
- [x] **Step 4: Commit**
  ```bash
  git add web/static/js/views/endpoints.js
  git commit -m "feat(ui): integrate quota fields in key modal and endpoints table"
  ```

---

### Task 8: Traffic Explorer Heavy Badges & Settings UI

**Files:**
- Modify: `web/static/js/views/traffic.js`
- Modify: `web/static/js/views/settings.js`

- [x] **Step 1: Traffic Explorer Badges**
  When request `total_tokens >= heavyThreshold`, show ⚠️ `Heavy (<N>k)` badge in row.
- [x] **Step 2: Settings UI**
  Add `Heavy Token Threshold` input under Settings.
- [x] **Step 3: Commit**
  ```bash
  git add web/static/js/views/traffic.js web/static/js/views/settings.js
  git commit -m "feat(ui): add heavy token warning badge in traffic explorer and settings input"
  ```

---

### Task 9: Full Regression Test & Verification

- [x] **Step 1: Run all Go unit & integration tests**
  Run: `CGO_ENABLED=0 go test -tags server ./...`
  Expected: All packages pass.
- [x] **Step 2: Run all JavaScript tests**
  Run: `node --test web/jstest/*.test.mjs`
  Expected: All suites pass.
- [x] **Step 3: Build server binary**
  Run: `go build -o nineguard.exe ./cmd/nineguard`
  Expected: Success.
