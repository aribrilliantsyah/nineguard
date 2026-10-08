# API Key Listing & Last Active Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every API timestamp RFC 3339 UTC, make Last Active all-time and timezone-correct, resolve report periods on the viewer's calendar, link traffic to API keys by ID, and give the key table server-side paging, sorting, search and filters.

**Architecture:** A new pure package `internal/timeutil` owns timestamp normalization and period math (`ResolvePeriod` → UTC instants bound as SQL parameters, replacing SQLite `'now'` modifiers). Phase 2 adds `traffic_logs.api_key_id` (migration + one-time backfill), writes it from the proxy, and switches every per-key aggregate to group by key ID with an "unlinked" fallback by historical name. `keys.ListKeysPage` does sorting/filtering/paging in SQL; the dashboard key table keeps its state in the URL hash, with pure helpers in `web/static/js/keylist.js`.

**Tech Stack:** Go 1.27.1, `modernc.org/sqlite` (pure Go), `net/http`, vanilla ES modules (no build step), `node --test` for JS helpers.

**Spec:** `docs/superpowers/specs/2026-10-06-key-listing-and-last-active-design.md` (ADR: `docs/adr/0003-traffic-linked-by-key-id.md`)

## Global Constraints

- Go `1.27.1`; binary must build and test with `CGO_ENABLED=0 go test -tags server ./...`. No new Go modules.
- Every timestamp in API JSON is RFC 3339 UTC ending in `Z`. Stored `traffic_logs.timestamp` format (`YYYY-MM-DD HH:MM:SS`, UTC) is **not** changed.
- Viewer timezone travels as the `tz` query parameter (IANA name). Missing/invalid → UTC. `time/tzdata` is embedded.
- Last Active = most recent request ever (not period-bound), any status code including 403.
- `GET /api/v1/keys` **without** `page` keeps the legacy `{"keys":[...]}` shape (Traffic Explorer dropdown, endpoint tester, scripts).
- Allowed values (verbatim from spec §4.5): `limit` 10/25/50/100 (clamped to nearest, default 25); `sort` `name|status|created|last_active|requests|tokens` (default `last_active`); `order` `asc|desc` (default `desc`); `status` `active|disabled|all`; `mode` `all|group|custom|any`. Invalid → 400 listing allowed values. Never-used keys sort last in both orders; ties `created_at DESC`, then `id`.
- JS tests live in `web/jstest/` (never under `web/static/`, which is embedded into the binary by `//go:embed static/*`).
- Never stage `package.json` / `package-lock.json` (stray, untracked). Stage files by explicit path only.
- Delivery: **Phase 1 (Tasks 1–8) ships and is deployed to the VM before Phase 2 (Tasks 9–18).**

### Deviations from the spec (intentional, decided while prototyping)

| Spec | Plan | Why |
|---|---|---|
| Helper "e.g. `internal/db/timefmt.go`" | Package `internal/timeutil` | Used by `db`, `keys`, `models`, `traffic`, `syslog`, `handler`; avoids import cycles and keeps `db` schema-only. |
| `Period{From, To}` with `To = now` | `Period` is half-open `[From, To)`; zero `To` = open-ended | Rows written after the query starts are still counted; `SQL()` emits `<` bounds so custom ranges end at next local midnight exactly. |
| Custom range end `23:59:59.999` | End = next local midnight, exclusive | Same instant, no sub-second edge cases. |

Prototype note: every code block in this plan was compiled and its tests run (`go test ./...`, `CGO_ENABLED=0 go test -tags server ./...`, `node --test`) against commit `66b1200`, and the UI was smoke-tested in headless Chrome.

## File Map

| File | Phase | Responsibility |
|---|---|---|
| `internal/timeutil/timeutil.go` (new) | 1 | Normalize SQLite timestamps; load viewer zone; `ResolvePeriod`; `Period.SQL`; strftime offset modifier |
| `internal/traffic/traffic.go` | 1, 2 | Period filters via `timeutil`; zone-shifted buckets; all-time Last Active; key-ID grouping; `key_id` filter |
| `internal/syslog/syslog.go` | 1 | Period filters via `timeutil` |
| `internal/keys/keys.go` | 1, 2 | Normalized `last_used_at`; stats joined by `api_key_id` |
| `internal/keys/page.go` (new) | 2 | `ListOptions`, `KeyPage`, `ListKeysPage` |
| `internal/models/models.go` | 1 | Normalized `last_used_at` |
| `internal/handler/handler.go` | 1, 2 | `tz` → `*time.Location`; paged `ListKeys`; `key_id` param |
| `internal/db/db.go` | 2 | `api_key_id` column, indexes, guarded backfill |
| `internal/proxy/proxy.go` | 2 | Record `APIKeyID` on every authenticated outcome |
| `cmd/nineguard/main.go` | 1, 2 | Embed `time/tzdata`; log backfill count |
| `web/static/js/api.js` | 1 | Add `tz` to reads/downloads; safe URL join |
| `web/static/js/views/reports.js` | 1, 2 | Relative Last Active; unlinked section |
| `web/static/js/keylist.js` (new) | 2 | Pure key-table state/paging helpers |
| `web/jstest/keylist.test.mjs` (new) | 2 | `node --test` for `keylist.js` |
| `web/static/js/views/endpoints.js` | 2 | Paged/sortable/filterable key table bound to URL hash |
| `web/static/js/views/dashboard.js`, `traffic.js`, `filters.js` | 2 | Filter traffic by `key_id` |

---

# Phase 1 — Time Correctness

### Task 1: `timeutil` package

**Files:**
- Create: `internal/timeutil/timeutil.go`
- Test: `internal/timeutil/timeutil_test.go`

**Interfaces:**
- Produces:
  - `const SQLiteLayout = "2006-01-02 15:04:05"`
  - `func NormalizeSQLiteTime(s string) string`
  - `func NullTimeString(ns sql.NullString) *string`
  - `func SQLiteTime(t time.Time) string`
  - `func LoadLocation(name string) *time.Location`
  - `type Period struct{ From, To time.Time }` and `func (p Period) SQL(col string) (string, []any)`
  - `func ResolvePeriod(period, start, end string, loc *time.Location, now time.Time) (cur, prev Period)`
  - `func SQLiteOffsetModifier(loc *time.Location, at time.Time) string`

- [ ] **Step 1: Write the failing test** — create `internal/timeutil/timeutil_test.go`:

```go
package timeutil

import (
	"database/sql"
	"testing"
	"time"
)

func TestNormalizeSQLiteTime(t *testing.T) {
	cases := map[string]string{
		"2026-10-06 09:26:40":       "2026-10-06T09:26:40Z",
		"2026-10-06 09:26:40.123":   "2026-10-06T09:26:40Z",
		"2026-10-06T09:26:40":       "2026-10-06T09:26:40Z",
		"2026-10-06T09:26:40Z":      "2026-10-06T09:26:40Z",
		"2026-10-06T16:26:40+07:00": "2026-10-06T09:26:40Z",
		"2026-10-06 09:26:40+00:00": "2026-10-06T09:26:40Z",
		" 2026-10-06 09:26:40 ":     "2026-10-06T09:26:40Z",
		"":                          "",
		"garbage":                   "",
		"2026-13-45 99:00:00":       "",
	}
	for in, want := range cases {
		if got := NormalizeSQLiteTime(in); got != want {
			t.Errorf("NormalizeSQLiteTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNullTimeString(t *testing.T) {
	if NullTimeString(sql.NullString{}) != nil {
		t.Error("NULL should map to nil")
	}
	if NullTimeString(sql.NullString{String: "junk", Valid: true}) != nil {
		t.Error("unparseable should map to nil")
	}
	got := NullTimeString(sql.NullString{String: "2026-10-06 09:26:40", Valid: true})
	if got == nil || *got != "2026-10-06T09:26:40Z" {
		t.Errorf("got %v", got)
	}
}

func TestLoadLocation(t *testing.T) {
	if LoadLocation("").String() != "UTC" || LoadLocation("Not/AZone").String() != "UTC" || LoadLocation("Local").String() != "UTC" {
		t.Error("expected UTC fallback")
	}
	if LoadLocation("Asia/Jakarta").String() != "Asia/Jakarta" {
		t.Error("expected Asia/Jakarta")
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestResolvePeriod(t *testing.T) {
	jkt := mustLoc(t, "Asia/Jakarta")
	ny := mustLoc(t, "America/New_York")
	// 2026-10-06 02:00 UTC = 09:00 in Jakarta, 22:00 previous day in New York.
	now := utc("2026-10-06T02:00:00Z")

	type tc struct {
		name, period, start, end string
		loc                      *time.Location
		now                      time.Time
		cur, prev                Period
	}
	cases := []tc{
		{"today utc", "today", "", "", time.UTC, now,
			Period{From: utc("2026-10-06T00:00:00Z")},
			Period{From: utc("2026-10-05T00:00:00Z"), To: utc("2026-10-06T00:00:00Z")}},
		{"today jakarta", "today", "", "", jkt, now,
			Period{From: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")}},
		{"empty period is today", "", "", "", jkt, now,
			Period{From: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")}},
		{"today new york (local date is still Oct 5)", "today", "", "", ny, now,
			Period{From: utc("2026-10-05T04:00:00Z")},
			Period{From: utc("2026-10-04T04:00:00Z"), To: utc("2026-10-05T04:00:00Z")}},
		{"yesterday jakarta", "yesterday", "", "", jkt, now,
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-03T17:00:00Z"), To: utc("2026-10-04T17:00:00Z")}},
		{"7d rolling", "7d", "", "", jkt, now,
			Period{From: utc("2026-09-29T02:00:00Z")},
			Period{From: utc("2026-09-22T02:00:00Z"), To: utc("2026-09-29T02:00:00Z")}},
		{"14d rolling", "14d", "", "", time.UTC, now,
			Period{From: utc("2026-09-22T02:00:00Z")},
			Period{From: utc("2026-09-08T02:00:00Z"), To: utc("2026-09-22T02:00:00Z")}},
		{"30d rolling", "30d", "", "", time.UTC, now,
			Period{From: utc("2026-09-06T02:00:00Z")},
			Period{From: utc("2026-08-07T02:00:00Z"), To: utc("2026-09-06T02:00:00Z")}},
		{"month jakarta", "month", "", "", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"this_month alias", "this_month", "", "", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"last_month jakarta", "last_month", "", "", jkt, now,
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-07-31T17:00:00Z"), To: utc("2026-08-31T17:00:00Z")}},
		{"month in new york is september (local date Oct 5? no: Oct 5 22:00)", "month", "", "", ny, now,
			Period{From: utc("2026-10-01T04:00:00Z")},
			Period{From: utc("2026-09-01T04:00:00Z"), To: utc("2026-10-01T04:00:00Z")}},
		{"all", "all", "", "", jkt, now,
			Period{},
			Period{From: now, To: now}},
		{"custom range jakarta", "custom", "2026-10-01", "2026-10-02", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z"), To: utc("2026-10-02T17:00:00Z")},
			Period{From: utc("2026-09-28T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"custom reversed dates are swapped", "custom", "2026-10-02", "2026-10-01", time.UTC, now,
			Period{From: utc("2026-10-01T00:00:00Z"), To: utc("2026-10-03T00:00:00Z")},
			Period{From: utc("2026-09-29T00:00:00Z"), To: utc("2026-10-01T00:00:00Z")}},
		{"start only", "", "2026-10-01", "", time.UTC, now,
			Period{From: utc("2026-10-01T00:00:00Z")},
			Period{From: now, To: now}},
		{"end only", "", "", "2026-10-01", time.UTC, now,
			Period{To: utc("2026-10-02T00:00:00Z")},
			Period{From: now, To: now}},
		{"invalid dates fall back to period", "7d", "2026-1-1", "nope", time.UTC, now,
			Period{From: utc("2026-09-29T02:00:00Z")},
			Period{From: utc("2026-09-22T02:00:00Z"), To: utc("2026-09-29T02:00:00Z")}},
		// DST: New York falls back on 2026-11-01 (EDT -4 -> EST -5).
		{"custom across DST new york", "custom", "2026-10-31", "2026-11-01", ny, now,
			Period{From: utc("2026-10-31T04:00:00Z"), To: utc("2026-11-02T05:00:00Z")},
			Period{From: utc("2026-10-29T04:00:00Z"), To: utc("2026-10-31T04:00:00Z")}},
		{"today on DST day new york", "today", "", "", ny, utc("2026-11-01T18:00:00Z"),
			Period{From: utc("2026-11-01T04:00:00Z")},
			Period{From: utc("2026-10-31T04:00:00Z"), To: utc("2026-11-01T04:00:00Z")}},
		{"nil loc is UTC", "today", "", "", nil, now,
			Period{From: utc("2026-10-06T00:00:00Z")},
			Period{From: utc("2026-10-05T00:00:00Z"), To: utc("2026-10-06T00:00:00Z")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur, prev := ResolvePeriod(c.period, c.start, c.end, c.loc, c.now)
			if !cur.From.Equal(c.cur.From) || !cur.To.Equal(c.cur.To) {
				t.Errorf("cur = [%v, %v), want [%v, %v)", cur.From, cur.To, c.cur.From, c.cur.To)
			}
			if !prev.From.Equal(c.prev.From) || !prev.To.Equal(c.prev.To) {
				t.Errorf("prev = [%v, %v), want [%v, %v)", prev.From, prev.To, c.prev.From, c.prev.To)
			}
		})
	}
}

func TestPeriodSQL(t *testing.T) {
	clause, args := Period{}.SQL("timestamp")
	if clause != "1=1" || len(args) != 0 {
		t.Errorf("unbounded: %q %v", clause, args)
	}
	clause, args = Period{From: utc("2026-10-05T17:00:00Z")}.SQL("t.timestamp")
	if clause != "t.timestamp >= datetime(?)" || len(args) != 1 || args[0] != "2026-10-05 17:00:00" {
		t.Errorf("from only: %q %v", clause, args)
	}
	clause, args = Period{From: utc("2026-10-05T17:00:00Z"), To: utc("2026-10-06T17:00:00Z")}.SQL("timestamp")
	if clause != "timestamp >= datetime(?) AND timestamp < datetime(?)" || len(args) != 2 || args[1] != "2026-10-06 17:00:00" {
		t.Errorf("both: %q %v", clause, args)
	}
}

func TestSQLiteOffsetModifier(t *testing.T) {
	if got := SQLiteOffsetModifier(mustLoc(t, "Asia/Jakarta"), utc("2026-10-06T00:00:00Z")); got != "+420 minutes" {
		t.Errorf("jakarta: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "America/New_York"), utc("2026-10-06T00:00:00Z")); got != "-240 minutes" {
		t.Errorf("new york summer: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "America/New_York"), utc("2026-12-06T00:00:00Z")); got != "-300 minutes" {
		t.Errorf("new york winter: %q", got)
	}
	if got := SQLiteOffsetModifier(time.UTC, time.Now()); got != "+0 minutes" {
		t.Errorf("utc: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "Asia/Kolkata"), time.Now()); got != "+330 minutes" {
		t.Errorf("kolkata: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/timeutil/ -v`
Expected: FAIL — build error, `undefined: NormalizeSQLiteTime` (package has no non-test files).

- [ ] **Step 3: Write the implementation** — create `internal/timeutil/timeutil.go`:

```go
// Package timeutil converts between SQLite text timestamps, API timestamps
// (RFC 3339 UTC) and viewer-local report periods.
package timeutil

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SQLiteLayout is the text format SQLite's CURRENT_TIMESTAMP produces (UTC, no zone).
const SQLiteLayout = "2006-01-02 15:04:05"

var sqliteParseLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
}

// NormalizeSQLiteTime converts SQLite text timestamps ("2006-01-02 15:04:05",
// with or without fractional seconds, "T" separator or zone) to RFC 3339 UTC
// ("2006-01-02T15:04:05Z"). Zoneless input is treated as UTC.
// Returns "" for empty or unparseable input.
func NormalizeSQLiteTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range sqliteParseLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// NullTimeString normalizes a nullable SQLite timestamp for JSON output.
// Returns nil when the value is NULL or unparseable.
func NullTimeString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := NormalizeSQLiteTime(ns.String)
	if v == "" {
		return nil
	}
	return &v
}

// SQLiteTime formats t as a UTC SQLite text timestamp for bound parameters.
func SQLiteTime(t time.Time) string {
	return t.UTC().Format(SQLiteLayout)
}

// LoadLocation resolves an IANA zone name sent by the viewer.
// Empty, "Local", or unknown names fall back to UTC.
func LoadLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" || len(name) > 64 {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Period is a half-open interval [From, To) of UTC instants.
// A zero From or To means the interval is unbounded on that side.
type Period struct {
	From time.Time
	To   time.Time
}

// SQL returns a WHERE fragment restricting col to the period, plus its
// arguments. An unbounded period returns "1=1" and no arguments.
func (p Period) SQL(col string) (string, []any) {
	var conds []string
	var args []any
	if !p.From.IsZero() {
		conds = append(conds, col+" >= datetime(?)")
		args = append(args, SQLiteTime(p.From))
	}
	if !p.To.IsZero() {
		conds = append(conds, col+" < datetime(?)")
		args = append(args, SQLiteTime(p.To))
	}
	if len(conds) == 0 {
		return "1=1", nil
	}
	return strings.Join(conds, " AND "), args
}

// ResolvePeriod returns the current and previous report windows for a period
// name, with calendar boundaries (midnight, first of month) in loc.
//
// Periods: today (default), yesterday, 7d, 14d, 30d, month (alias this_month),
// last_month, all. When start and/or end ("YYYY-MM-DD", local dates) are valid
// they override period: [start 00:00, end+1 day 00:00) in loc.
func ResolvePeriod(period, start, end string, loc *time.Location, now time.Time) (cur, prev Period) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	y, m, d := local.Date()
	midnight := func(addDays int) time.Time {
		return time.Date(y, m, d+addDays, 0, 0, 0, 0, loc).UTC()
	}
	monthStart := func(addMonths int) time.Time {
		return time.Date(y, m+time.Month(addMonths), 1, 0, 0, 0, 0, loc).UTC()
	}
	empty := Period{From: now.UTC(), To: now.UTC()}

	s, sOK := parseDay(start, loc)
	e, eOK := parseDay(end, loc)
	switch {
	case sOK && eOK:
		if e.Before(s) {
			s, e = e, s
		}
		endExcl := e.AddDate(0, 0, 1)
		days := 0
		for t := s; t.Before(endExcl); t = t.AddDate(0, 0, 1) {
			days++
		}
		cur = Period{From: s.UTC(), To: endExcl.UTC()}
		prev = Period{From: s.AddDate(0, 0, -days).UTC(), To: s.UTC()}
		return cur, prev
	case sOK:
		return Period{From: s.UTC()}, empty
	case eOK:
		return Period{To: e.AddDate(0, 0, 1).UTC()}, empty
	}

	switch period {
	case "yesterday":
		return Period{From: midnight(-1), To: midnight(0)}, Period{From: midnight(-2), To: midnight(-1)}
	case "7d", "14d", "30d":
		n := map[string]int{"7d": 7, "14d": 14, "30d": 30}[period]
		span := time.Duration(n) * 24 * time.Hour
		nowUTC := now.UTC()
		return Period{From: nowUTC.Add(-span)}, Period{From: nowUTC.Add(-2 * span), To: nowUTC.Add(-span)}
	case "month", "this_month":
		return Period{From: monthStart(0)}, Period{From: monthStart(-1), To: monthStart(0)}
	case "last_month":
		return Period{From: monthStart(-1), To: monthStart(0)}, Period{From: monthStart(-2), To: monthStart(-1)}
	case "all":
		return Period{}, empty
	default: // "today" and unknown values
		return Period{From: midnight(0)}, Period{From: midnight(-1), To: midnight(0)}
	}
}

func parseDay(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 10 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// SQLiteOffsetModifier returns a strftime modifier such as "+420 minutes" that
// shifts UTC timestamps into loc, using loc's UTC offset at instant at.
// DST changes inside a range may shift buckets after the change by one hour.
func SQLiteOffsetModifier(loc *time.Location, at time.Time) string {
	if loc == nil {
		loc = time.UTC
	}
	_, off := at.In(loc).Zone()
	return fmt.Sprintf("%+d minutes", off/60)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/timeutil/ -v`
Expected: PASS (all subtests of `TestResolvePeriod`, including the New York DST cases).

- [ ] **Step 5: Commit**

```bash
git add internal/timeutil/timeutil.go internal/timeutil/timeutil_test.go
git commit -m "feat(timeutil): timestamp normalization and viewer-timezone periods"
```

---

### Task 2: Traffic periods and chart buckets in the viewer's timezone

**Files:**
- Modify: `internal/traffic/traffic.go` (`FilterParams`, `buildDateFilter`, `buildPrevDateFilter`, `QueryLogs`, `GetVolume`, `GetDashboardStats`, `GetUsageReports` signature)
- Modify: `internal/traffic/traffic_test.go` (`TestDateFilterParameterized`)
- Modify: `internal/handler/handler.go` (two call sites only, to keep the build green; real `tz` wiring is Task 6)
- Test: `internal/traffic/timezone_test.go`

**Interfaces:**
- Consumes: `timeutil.ResolvePeriod`, `timeutil.Period.SQL`, `timeutil.SQLiteOffsetModifier` (Task 1).
- Produces:
  - `traffic.FilterParams.Loc *time.Location`
  - `func (m *Manager) GetDashboardStats(period, startDate, endDate string, loc *time.Location) (*DashboardStats, error)`
  - `func (m *Manager) GetUsageReports(period, startDate, endDate string, loc *time.Location) (*UsageReport, error)`
  - package-level `var nowFunc = time.Now` (tests replace it)
  - `func periodVolumeWindow(period, startDate, endDate string, loc *time.Location, to time.Time) (time.Time, time.Time)`
  - Test helpers in package `traffic`: `insertAt(t, mgr, ts, keyName, model string, status, tokens int)`, `fixNow(t, rfc3339 string)`

- [ ] **Step 1: Write the failing test** — create `internal/traffic/timezone_test.go`:

```go
package traffic

import (
	"testing"
	"time"
)

// insertAt writes a traffic row with an explicit UTC timestamp.
func insertAt(t *testing.T, mgr *Manager, ts, keyName, model string, status, tokens int) {
	t.Helper()
	_, err := mgr.db.Exec(`
		INSERT INTO traffic_logs (timestamp, api_key, api_key_name, model, total_tokens, status_code, client_ip, level)
		VALUES (?, 'sk-ng-...abcd', ?, ?, ?, ?, '127.0.0.1', '')`,
		ts, keyName, model, tokens, status)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func fixNow(t *testing.T, rfc3339 string) {
	t.Helper()
	fixed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	t.Cleanup(func() { nowFunc = old })
}

func TestTodayUsesViewerTimezone(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	// 02:00 UTC = 09:00 in Jakarta. Jakarta "today" starts 2026-10-05 17:00 UTC.
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	insertAt(t, mgr, "2026-10-05 16:59:59", "a", "m", 200, 1) // Jakarta: Oct 5 23:59:59 (yesterday)
	insertAt(t, mgr, "2026-10-05 17:00:00", "a", "m", 200, 1) // Jakarta: Oct 6 00:00 (today)
	insertAt(t, mgr, "2026-10-06 01:30:00", "a", "m", 200, 1) // Jakarta: Oct 6 08:30 (today)

	stats, err := mgr.GetDashboardStats("today", "", "", jkt)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 2 {
		t.Errorf("jakarta today: got %d requests, want 2", stats.TotalRequests)
	}
	if stats.Comparison.PrevRequests != 1 {
		t.Errorf("jakarta yesterday: got %d, want 1", stats.Comparison.PrevRequests)
	}
	// Hourly buckets are labelled in Jakarta time: 00:00 and 08:00.
	got := map[string]int{}
	for _, p := range stats.VolumeSeries {
		got[p.Time] = p.Requests
	}
	if got["00:00"] != 1 || got["08:00"] != 1 || got["17:00"] != 0 {
		t.Errorf("buckets not shifted to Jakarta: %v", got)
	}

	statsUTC, err := mgr.GetDashboardStats("today", "", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if statsUTC.TotalRequests != 1 {
		t.Errorf("utc today: got %d, want 1", statsUTC.TotalRequests)
	}
}

func TestQueryLogsPeriodUsesLoc(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	insertAt(t, mgr, "2026-10-05 16:59:59", "a", "m", 200, 1)
	insertAt(t, mgr, "2026-10-05 17:00:00", "a", "m", 200, 1)

	_, total, err := mgr.QueryLogs(FilterParams{Period: "today", Loc: jkt})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "yesterday", Loc: jkt})
	if total != 1 {
		t.Errorf("yesterday: got %d, want 1", total)
	}
}

func TestPeriodVolumeWindow(t *testing.T) {
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := nowFunc()

	from, to := periodVolumeWindow("today", "", "", jkt, now)
	if !from.Equal(time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 6, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("today: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("yesterday", "", "", jkt, now)
	if !from.Equal(time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 5, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("yesterday: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("7d", "", "", jkt, now)
	if !from.Equal(now.Add(-7*24*time.Hour)) || !to.Equal(now) {
		t.Errorf("7d: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("", "2026-10-01", "2026-10-01", jkt, now)
	if !from.Equal(time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 1, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("custom: %v .. %v", from, to)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/traffic/ -run 'Timezone|Loc|PeriodVolumeWindow' -v`
Expected: FAIL — build errors: `undefined: nowFunc`, `unknown field Loc in struct literal`, `too many arguments in call to mgr.GetDashboardStats`, `undefined: periodVolumeWindow`.

- [ ] **Step 3: Add the import and `Loc` field**

In `internal/traffic/traffic.go` imports, add `"nineguard/internal/timeutil"` after `"nineguard/internal/db"`. In `type FilterParams`, after `Cursor`:

```go
	Cursor    string         // id cursor for pagination
	Loc       *time.Location // viewer timezone for Period/StartDate/EndDate; nil = UTC
```

- [ ] **Step 4: Replace the date filter builders**

Replace the whole of `buildDateFilter` and `buildPrevDateFilter` (everything between `sanitizeDate` and `type Manager struct`) with:

```go
// nowFunc is the clock used for period resolution; tests may replace it.
var nowFunc = time.Now

// buildDateFilter returns the WHERE fragment for the current report window,
// with calendar boundaries in loc (nil = UTC).
func buildDateFilter(period, startDate, endDate string, loc *time.Location) (string, []interface{}) {
	cur, _ := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	return cur.SQL("timestamp")
}

// buildPrevDateFilter returns the WHERE fragment for the window preceding the
// current one, used for period-over-period comparison.
func buildPrevDateFilter(period, startDate, endDate string, loc *time.Location) (string, []interface{}) {
	_, prev := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	return prev.SQL("timestamp")
}
```

- [ ] **Step 5: Thread `Loc` through `QueryLogs` and `GetVolume`**

In `QueryLogs`, change `buildDateFilter(p.Period, p.StartDate, p.EndDate)` to `buildDateFilter(p.Period, p.StartDate, p.EndDate, p.Loc)`.

In `GetVolume`, replace the entire `if from, ok = parseTimeParam(p.From); !ok { ... }` block (the one containing `if p.StartDate != "" && p.EndDate != ""` and the `switch p.Period`) with:

```go
	if from, ok = parseTimeParam(p.From); !ok {
		from, to = periodVolumeWindow(p.Period, p.StartDate, p.EndDate, p.Loc, to)
	}
```

Append to the end of `traffic.go`:

```go

```

- [ ] **Step 6: `GetDashboardStats` / `GetUsageReports` take `loc`**

Replace the first lines of each function:

```go
func (m *Manager) GetDashboardStats(period, startDate, endDate string, loc *time.Location) (*DashboardStats, error) {
	if loc == nil {
		loc = time.UTC
	}
	dateFilter, dateFilterArgs := buildDateFilter(period, startDate, endDate, loc)
	prevFilter, prevFilterArgs := buildPrevDateFilter(period, startDate, endDate, loc)
```

```go
func (m *Manager) GetUsageReports(period, startDate, endDate string, loc *time.Location) (*UsageReport, error) {
	if loc == nil {
		loc = time.UTC
	}
	dateFilter, dateFilterArgs := buildDateFilter(period, startDate, endDate, loc)
```

- [ ] **Step 7: Zone-shifted time-series buckets in `GetDashboardStats`**

Replace the block from `	// Time series setup` up to (not including) `	slotMap := make(map[string]*TimeSeriesPoint)` with:

```go
	// Time series setup. Bucket labels are in the viewer's zone: SQLite
	// timestamps (UTC) are shifted by loc's offset at the start of the window.
	var timeFmt string
	var timeSlots []string
	now := nowFunc().In(loc)
	curPeriod, _ := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	offsetAt := curPeriod.From
	if offsetAt.IsZero() {
		offsetAt = now
	}
	offsetMod := timeutil.SQLiteOffsetModifier(loc, offsetAt)

	if period == "today" || period == "" {
		timeFmt = "%H:00"
		for h := 0; h < 24; h++ {
			timeSlots = append(timeSlots, fmt.Sprintf("%02d:00", h))
		}
	} else if startDate != "" && endDate != "" {
		timeFmt = "%m-%d"
		t1, err1 := time.Parse("2006-01-02", startDate)
		t2, err2 := time.Parse("2006-01-02", endDate)
		if err1 == nil && err2 == nil && !t2.Before(t1) {
			for cur := t1; !cur.After(t2); cur = cur.AddDate(0, 0, 1) {
				timeSlots = append(timeSlots, cur.Format("01-02"))
			}
		} else {
			for d := 13; d >= 0; d-- {
				timeSlots = append(timeSlots, now.AddDate(0, 0, -d).Format("01-02"))
			}
		}
	} else {
		timeFmt = "%m-%d"
		days := 14
		switch period {
		case "7d":
			days = 7
		case "14d":
			days = 14
		case "30d":
			days = 30
		}
		for d := days - 1; d >= 0; d-- {
			timeSlots = append(timeSlots, now.AddDate(0, 0, -d).Format("01-02"))
		}
	}
	// slotExpr is the SQL expression producing a bucket label for a row.
	slotExpr := fmt.Sprintf("strftime('%s', timestamp, '%s')", timeFmt, offsetMod)
	groupFmt := slotExpr
```

Then, in the five bucketed queries below it in `GetDashboardStats`, use `slotExpr` instead of `strftime('%s', timestamp)` + `timeFmt`:

| Query variable | SQL: before → after | `fmt.Sprintf` args: before → after |
|---|---|---|
| `seriesQuery` | `strftime('%s', timestamp) as time_slot` → `%s as time_slot` | `timeFmt, dateFilter, groupFmt` → `slotExpr, dateFilter, groupFmt` |
| `trendQ` (top models) | `SELECT model, strftime('%s', timestamp) as ts` → `SELECT model, %s as ts` | `timeFmt, dateFilter` → `slotExpr, dateFilter` |
| `trendKeyQ` (top keys) | `..., strftime('%s', timestamp) as ts` → `..., %s as ts` | `timeFmt, dateFilter` → `slotExpr, dateFilter` |
| `trendErrQ` (error sources) | `SELECT model, strftime('%s', timestamp) as ts` → `SELECT model, %s as ts` | `timeFmt, dateFilter` → `slotExpr, dateFilter` |
| `keySeriesQuery` | `strftime('%s', timestamp) as time_slot` → `%s as time_slot` | `timeFmt, dateFilter, groupFmt` → `slotExpr, dateFilter, groupFmt` |

After this step `grep -n "strftime('%s', timestamp)" internal/traffic/traffic.go` must print nothing.

- [ ] **Step 8: Update the existing date-filter test**

In `internal/traffic/traffic_test.go` `TestDateFilterParameterized`, replace the first two assertions and the two manager calls:

```go
	clause, args := buildDateFilter("", "2026-01-01", "2026-01-02", nil)
	if clause != "timestamp >= datetime(?) AND timestamp < datetime(?)" {
		t.Fatalf("expected parameterized clause with '?', got %q", clause)
	}
	if len(args) != 2 || args[0] != "2026-01-01 00:00:00" || args[1] != "2026-01-03 00:00:00" {
		t.Fatalf("unexpected date filter args: %v", args)
	}

	// 2. Verify buildPrevDateFilter returns ? placeholders
	prevClause, prevArgs := buildPrevDateFilter("", "2026-01-01", "2026-01-02", nil)
	if prevClause != "timestamp >= datetime(?) AND timestamp < datetime(?)" {
```

and `mgr.GetDashboardStats("today", "", "")` → `mgr.GetDashboardStats("today", "", "", nil)`, `mgr.GetUsageReports("today", "", "")` → `mgr.GetUsageReports("today", "", "", nil)`.

- [ ] **Step 9: Keep the handler compiling**

In `internal/handler/handler.go`: `h.traffic.GetDashboardStats(period, startDate, endDate)` → `h.traffic.GetDashboardStats(period, startDate, endDate, nil)` and `h.traffic.GetUsageReports(period, startDate, endDate)` → `h.traffic.GetUsageReports(period, startDate, endDate, nil)` (Task 6 replaces `nil`).

- [ ] **Step 10: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/traffic/ -v`
Expected: PASS — `TestTodayUsesViewerTimezone`, `TestQueryLogsPeriodUsesLoc`, `TestPeriodVolumeWindow`, `TestDateFilterParameterized`, `TestRecordAndQueryLogs`, `TestComputeLevelAndMessage`.

- [ ] **Step 11: Commit**

```bash
git add internal/traffic/traffic.go internal/traffic/traffic_test.go internal/traffic/timezone_test.go internal/handler/handler.go
git commit -m "fix(traffic): resolve report periods and chart buckets in viewer timezone"
```

---

### Task 3: Usage Reports — Last Active is all-time and RFC 3339

**Files:**
- Modify: `internal/traffic/traffic.go` (`GetUsageReports` steps 3–4, new `lastActiveBy`)
- Test: `internal/traffic/last_active_test.go`

**Interfaces:**
- Consumes: `timeutil.NullTimeString` (Task 1); `insertAt`, `fixNow` (Task 2).
- Produces: `func (m *Manager) lastActiveBy(groupExpr string) map[string]*string` (Task 14 changes the signature to `lastActiveBy(groupExpr, join string)`).

- [ ] **Step 1: Write the failing test** — create `internal/traffic/last_active_test.go`:

```go
package traffic

import (
	"strings"
	"testing"
	"time"
)

func TestUsageReportLastActiveIgnoresPeriod(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")

	insertAt(t, mgr, "2026-09-15 08:00:00", "pi-dev", "9r/claude", 200, 100) // last month
	insertAt(t, mgr, "2026-10-06 09:26:40", "pi-dev", "9r/claude", 403, 0)   // today, blocked

	report, err := mgr.GetUsageReports("last_month", "", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.KeysBreakdown) != 1 {
		t.Fatalf("expected 1 key, got %d", len(report.KeysBreakdown))
	}
	k := report.KeysBreakdown[0]
	if k.TotalRequests != 1 {
		t.Errorf("period stats should only count last month: got %d requests", k.TotalRequests)
	}
	if k.LastActiveAt == nil || *k.LastActiveAt != "2026-10-06T09:26:40Z" {
		t.Errorf("key last_active_at = %v, want 2026-10-06T09:26:40Z (all-time, includes 403)", k.LastActiveAt)
	}
	if len(report.ModelsBreakdown) != 1 {
		t.Fatalf("expected 1 model, got %d", len(report.ModelsBreakdown))
	}
	if la := report.ModelsBreakdown[0].LastActiveAt; la == nil || *la != "2026-10-06T09:26:40Z" {
		t.Errorf("model last_active_at = %v", la)
	}
}

func TestUsageReportTimestampsAreRFC3339UTC(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	_ = mgr.Record(&LogEntry{APIKey: "sk-ng-test-1234567", APIKeyName: "k", Model: "m", StatusCode: 200})
	report, err := mgr.GetUsageReports("all", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	la := report.KeysBreakdown[0].LastActiveAt
	if la == nil || !strings.HasSuffix(*la, "Z") || !strings.Contains(*la, "T") {
		t.Errorf("last_active_at not RFC 3339 UTC: %v", la)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/traffic/ -run 'LastActive|RFC3339' -v`
Expected: FAIL — `key last_active_at = <ptr>, want 2026-10-06T09:26:40Z (all-time, includes 403)` (current value is the period-bound, zoneless `2026-09-15 08:00:00`), and `last_active_at not RFC 3339 UTC`.

- [ ] **Step 3: Implement**

In `GetUsageReports`, insert before the `// 3. Query Grouping per API Key` comment (and renumber that comment to `// 4.` and the models one to `// 5.`):

```go
	// 3. Last Active per key and per model: all-time, any status (not period-bound).
	keyLastActive := m.lastActiveBy(`COALESCE(NULLIF(api_key_name, ''), api_key) || '||' || api_key`)
	modelLastActive := m.lastActiveBy("model")
```

In `keysQuery`, change `COALESCE(ROUND(AVG(duration_ms)), 0) as avg_dur,` + `MAX(timestamp) as last_seen` to just `COALESCE(ROUND(AVG(duration_ms)), 0) as avg_dur`. In its scan loop: delete `var lastSeen sql.NullString`, drop `&lastSeen` from `Scan(...)`, delete the `if lastSeen.Valid { kb.LastActiveAt = &lastSeen.String }` block, and after `kId := kb.KeyName + "||" + kb.Key` add:

```go
				kb.LastActiveAt = keyLastActive[kId]
```

In `modelsQuery`, same removal of `MAX(t.timestamp) as last_seen` (and the comma before it). In its scan loop: delete `var lastSeen sql.NullString`, drop `&lastSeen`, and replace the `if lastSeen.Valid { ... }` block with:

```go
				mb.LastActiveAt = modelLastActive[mb.Model]
```

Append to `traffic.go`:

```go
// lastActiveBy returns MAX(timestamp) over all traffic (no period filter, all
// status codes) grouped by the given SQL expression, normalized to RFC 3339 UTC.
// groupExpr must be a trusted constant expression, never user input.
func (m *Manager) lastActiveBy(groupExpr string) map[string]*string {
	out := make(map[string]*string)
	rows, err := m.db.Query(fmt.Sprintf(
		"SELECT %s AS g, MAX(timestamp) FROM traffic_logs GROUP BY g", groupExpr))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g sql.NullString
		var ts sql.NullString
		if err := rows.Scan(&g, &ts); err == nil && g.Valid {
			out[g.String] = timeutil.NullTimeString(ts)
		}
	}
	return out
}

// periodVolumeWindow converts a period into the [from, to] span used by volume
// histograms, with calendar boundaries in loc. "today" spans the whole local
// day so the chart has a stable width; open-ended windows end at to.
func periodVolumeWindow(period, startDate, endDate string, loc *time.Location, to time.Time) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	cur, _ := timeutil.ResolvePeriod(period, startDate, endDate, loc, nowFunc())
	from := cur.From
	if !cur.To.IsZero() {
		to = cur.To.Add(-time.Second)
	} else if !from.IsZero() && (period == "" || period == "today") && startDate == "" && endDate == "" {
		to = from.In(loc).AddDate(0, 0, 1).UTC().Add(-time.Second)
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/traffic/ -v`
Expected: PASS (all tests, including Task 2's).

- [ ] **Step 5: Commit**

```bash
git add internal/traffic/traffic.go internal/traffic/last_active_test.go
git commit -m "fix(reports): Last Active is all-time, any status, RFC 3339 UTC"
```

---

### Task 4: Normalize `last_used_at` for keys and models

**Files:**
- Modify: `internal/keys/keys.go` (`ListKeys`), `internal/models/models.go` (`ListModels`)
- Test: `internal/keys/lastused_test.go`

**Interfaces:**
- Consumes: `timeutil.NullTimeString` (Task 1).

- [ ] **Step 1: Write the failing test** — create `internal/keys/lastused_test.go`:

```go
package keys_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
)

func TestLastUsedAtIsRFC3339UTC(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	km := keys.NewManager(database, "")
	k, err := km.CreateKey("pi-dev", "all", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO models (id, name, enabled) VALUES ('9r/claude', '9r/claude', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, model, status_code)
		VALUES ('2026-10-06 09:26:40', ?, 'pi-dev', '9r/claude', 403)`, k.Key); err != nil {
		t.Fatal(err)
	}

	list, err := km.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ki := range list {
		if ki.ID == k.ID {
			found = true
			if ki.LastUsedAt == nil || *ki.LastUsedAt != "2026-10-06T09:26:40Z" {
				t.Errorf("key last_used_at = %v, want 2026-10-06T09:26:40Z", ki.LastUsedAt)
			}
		}
	}
	if !found {
		t.Fatal("created key not listed")
	}

	ml, err := models.NewManager(database).ListModels("")
	if err != nil {
		t.Fatal(err)
	}
	for _, mi := range ml {
		if mi.ID == "9r/claude" {
			if mi.LastUsedAt == nil || *mi.LastUsedAt != "2026-10-06T09:26:40Z" {
				t.Errorf("model last_used_at = %v, want 2026-10-06T09:26:40Z", mi.LastUsedAt)
			}
			return
		}
	}
	t.Fatal("model 9r/claude not listed")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keys/ -run LastUsed -v`
Expected: FAIL — `key last_used_at = 0x..., want 2026-10-06T09:26:40Z` (value is `2026-10-06 09:26:40` without zone).

- [ ] **Step 3: Implement**

In both files add import `"nineguard/internal/timeutil"` after `"nineguard/internal/db"`. In `keys.go` `ListKeys` replace

```go
		if lastUsed.Valid {
			ki.LastUsedAt = &lastUsed.String
		}
```

with `		ki.LastUsedAt = timeutil.NullTimeString(lastUsed)`. In `models.go` `ListModels` replace the equivalent `mi.LastUsedAt` block with `		mi.LastUsedAt = timeutil.NullTimeString(lastUsed)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/keys/ ./internal/models/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/keys/keys.go internal/models/models.go internal/keys/lastused_test.go
git commit -m "fix(keys,models): last_used_at as RFC 3339 UTC"
```

---

### Task 5: System log periods in the viewer's timezone

**Files:**
- Modify: `internal/syslog/syslog.go` (`FilterParams`, `QueryLogs`, `GetVolume`)
- Test: `internal/syslog/timezone_test.go`

**Interfaces:**
- Consumes: `timeutil.ResolvePeriod`, `Period.SQL` (Task 1).
- Produces: `syslog.FilterParams.Loc *time.Location`; package-level `var nowFunc = time.Now`.

- [ ] **Step 1: Write the failing test** — create `internal/syslog/timezone_test.go`:

```go
package syslog

import (
	"testing"
	"time"
)

func TestQueryLogsPeriodUsesLoc(t *testing.T) {
	mgr, cleanup := setupTestSyslogDB(t)
	defer cleanup()

	fixed := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC) // 09:00 in Jakarta
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	defer func() { nowFunc = old }()

	for _, ts := range []string{"2026-10-05 16:59:59", "2026-10-05 17:00:00"} {
		if _, err := mgr.db.Exec(`INSERT INTO system_logs (timestamp, level, source, message) VALUES (?, 'INFO', 'test', 'm')`, ts); err != nil {
			t.Fatal(err)
		}
	}
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	_, total, err := mgr.QueryLogs(FilterParams{Period: "today", Loc: jkt})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("jakarta today: got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "yesterday", Loc: jkt})
	if total != 1 {
		t.Errorf("jakarta yesterday: got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "today"})
	if total != 0 {
		t.Errorf("utc today: got %d, want 0", total)
	}

	vol, err := mgr.GetVolume(FilterParams{Period: "today", Loc: jkt}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if vol.From != time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC).UnixNano() {
		t.Errorf("volume from = %v", time.Unix(0, vol.From).UTC())
	}
	if vol.Totals["INFO"] != 1 {
		t.Errorf("volume INFO total = %d, want 1", vol.Totals["INFO"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syslog/ -run Loc -v`
Expected: FAIL — `undefined: nowFunc`, `unknown field Loc`.

- [ ] **Step 3: Implement**

Add import `"nineguard/internal/timeutil"`. In `FilterParams` add after `Cursor`:

```go
	Loc       *time.Location // viewer timezone for Period/StartDate/EndDate; nil = UTC
```

and directly after the `FilterParams` type:

```go
// nowFunc is the clock used for period resolution; tests may replace it.
var nowFunc = time.Now
```

In `QueryLogs`, replace the body of `if p.From == "" && p.To == "" { ... }` (the `StartDate`/`switch p.Period` logic) so the block reads:

```go
	if p.From == "" && p.To == "" {
		cur, _ := timeutil.ResolvePeriod(p.Period, p.StartDate, p.EndDate, p.Loc, nowFunc())
		if cond, condArgs := cur.SQL("timestamp"); cond != "1=1" {
			conditions = append(conditions, cond)
			args = append(args, condArgs...)
		}
	}
```

In `GetVolume`, replace the whole `if from, ok = parseTimeParam(p.From); !ok { ... }` block with:

```go
	if from, ok = parseTimeParam(p.From); !ok {
		loc := p.Loc
		if loc == nil {
			loc = time.UTC
		}
		cur, _ := timeutil.ResolvePeriod(p.Period, p.StartDate, p.EndDate, loc, nowFunc())
		from = cur.From
		if !cur.To.IsZero() {
			to = cur.To.Add(-time.Second)
		} else if !from.IsZero() && (p.Period == "" || p.Period == "today") && p.StartDate == "" && p.EndDate == "" {
			to = from.In(loc).AddDate(0, 0, 1).UTC().Add(-time.Second)
		}
		if from.IsZero() {
			from = to.Add(-24 * time.Hour)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/syslog/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/syslog/syslog.go internal/syslog/timezone_test.go
git commit -m "fix(syslog): resolve log periods in viewer timezone"
```

---

### Task 6: Handlers accept `tz`; embed tzdata

**Files:**
- Modify: `internal/handler/handler.go`, `cmd/nineguard/main.go`
- Test: `internal/handler/timezone_test.go`

**Interfaces:**
- Consumes: `timeutil.LoadLocation`; `FilterParams.Loc` (Tasks 2, 5); new `GetDashboardStats`/`GetUsageReports` signatures.
- Produces: `func requestLocation(r *http.Request) *time.Location` (unexported).

- [ ] **Step 1: Write the failing test** — create `internal/handler/timezone_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/traffic"
)

func TestUsageReportAndStatsAcceptTZ(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "tz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tm := traffic.NewManager(database)
	h := handler.New(nil, nil, tm, nil, nil, nil, nil, "")

	// A row 1 minute after Jakarta midnight today, which is 17:01 UTC "yesterday".
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	jktMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, jkt).UTC()
	if _, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, model, status_code, client_ip)
		VALUES (?, 'sk-ng-...abcd', 'pi-dev', 'm', 200, '127.0.0.1')`, jktMidnight.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tz   string
		want int
	}{{"Asia/Jakarta", 1}, {"Not/AZone", -1}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/report?period=today&tz="+tc.tz, nil)
		w := httptest.NewRecorder()
		h.GetUsageReport(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tz=%s: status %d %s", tc.tz, w.Code, w.Body.String())
		}
		var rep traffic.UsageReport
		if err := json.NewDecoder(w.Body).Decode(&rep); err != nil {
			t.Fatal(err)
		}
		if tc.want >= 0 && rep.TotalRequests != tc.want {
			t.Errorf("tz=%s: total %d, want %d", tc.tz, rep.TotalRequests, tc.want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/stats?period=today&tz=Asia/Jakarta", nil)
	w := httptest.NewRecorder()
	h.GetTrafficStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats status %d", w.Code)
	}
	var stats traffic.DashboardStats
	_ = json.NewDecoder(w.Body).Decode(&stats)
	if stats.TotalRequests != 1 {
		t.Errorf("stats jakarta today: %d, want 1", stats.TotalRequests)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/traffic?period=today&tz=Asia/Jakarta", nil)
	w = httptest.NewRecorder()
	h.GetTrafficLogs(w, req)
	var logs struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&logs)
	if logs.Total != 1 {
		t.Errorf("traffic logs jakarta today: %d, want 1", logs.Total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run TZ -v`
Expected: FAIL — `tz=Asia/Jakarta: total 0, want 1` (handler still passes `nil` = UTC). Note: before 07:00 Jakarta time this row is also "today" in UTC and the first assertion may pass; the `stats` and `traffic logs` assertions still fail until Step 3.

- [ ] **Step 3: Implement**

`handler.go`: add import `"nineguard/internal/timeutil"`. Add above `parseSyslogFilterParams`:

```go
// requestLocation returns the viewer's timezone from the "tz" query parameter
// (IANA name, e.g. "Asia/Jakarta"). Missing or invalid values yield UTC.
func requestLocation(r *http.Request) *time.Location {
	return timeutil.LoadLocation(r.URL.Query().Get("tz"))
}
```

In both `parseSyslogFilterParams` and `parseFilterParams`, add as the last struct field:

```go
		Loc:       timeutil.LoadLocation(q.Get("tz")),
```

Replace the two `nil` arguments from Task 2 Step 9 with `requestLocation(r)`.

`cmd/nineguard/main.go`: in the import block, after `"time"`:

```go
	_ "time/tzdata" // embed zoneinfo so viewer timezones resolve without system tzdata
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -v && CGO_ENABLED=0 go build -tags server -o /dev/null ./cmd/nineguard`
Expected: PASS; build succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/handler.go internal/handler/timezone_test.go cmd/nineguard/main.go
git commit -m "feat(api): accept viewer tz on period endpoints; embed tzdata"
```

---

### Task 7: Frontend sends `tz`; Reports show relative Last Active

**Files:**
- Modify: `web/static/js/api.js`, `web/static/js/views/reports.js`

**Interfaces:**
- Consumes: `timeZone()`, `fmtAgo(ms)`, `fmtDateTime(ms)`, `tzLabel()` from `web/static/js/ui.js` (existing).
- Produces: every `api.get(...)` and `api.download(...)` carries `tz`; internal `url(path, params)` joins with `&` when `path` already has `?` (fixes `reports.js` export, which built `/traffic?export=csv?period=...`).

- [ ] **Step 1: Replace `web/static/js/api.js`** with:

```js
// REST client for the server API. Authentication is the HttpOnly session
// cookie set by /login, so requests carry no token of their own.
import { timeZone } from './ui.js';

// Every read carries the viewer's IANA timezone so the server resolves
// report periods ("today", "this month") on the viewer's calendar.
const withTZ = (params) => ({ tz: timeZone(), ...(params || {}) });

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

export function qs(params = {}) {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') u.set(k, v);
  }
  const s = u.toString();
  return s ? '?' + s : '';
}

// Builds an API URL; paths that already carry a query string get "&" params.
function url(path, params) {
  const q = qs(params);
  return '/api/v1' + path + (q && path.includes('?') ? '&' + q.slice(1) : q);
}

// Session gone: go to the sign-in page and come back to the same view after.
export function redirectToLogin() {
  location.href = '/login?next=' + encodeURIComponent(location.pathname + location.search) + location.hash;
}

async function request(method, path, { params, body, timeout = 12000 } = {}) {
  let resp;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  try {
    resp = await fetch(url(path, params), {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: controller.signal,
    });
  } catch (err) {
    if (err.name === 'AbortError') {
      throw new ApiError(0, 'Request timed out — server took too long to respond');
    }
    throw new ApiError(0, 'Cannot reach the NineGuard server');
  } finally {
    clearTimeout(timer);
  }
  if (resp.status === 401 && !path.startsWith('/auth/')) {
    redirectToLogin();
    throw new ApiError(401, 'Sign in required');
  }
  const text = await resp.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* non-JSON body */ }
  if (!resp.ok) throw new ApiError(resp.status, (data && data.error) || `Request failed (HTTP ${resp.status})`);
  return data;
}

export const api = {
  get: (path, params) => request('GET', path, { params: withTZ(params) }),
  post: (path, body = {}) => request('POST', path, { body }),
  put: (path, body = {}) => request('PUT', path, { body }),
  patch: (path, body = {}) => request('PATCH', path, { body }),
  del: (path, params) => request('DELETE', path, { params }),

  // Downloads a server-generated file (export).
  async download(path, params) {
    const resp = await fetch(url(path, withTZ(params)));
    if (resp.status === 401) return redirectToLogin();
    if (!resp.ok) {
      let msg = `Export failed (HTTP ${resp.status})`;
      try { msg = (await resp.json()).error || msg; } catch { /* keep default */ }
      throw new ApiError(resp.status, msg);
    }
    const name = /filename="([^"]+)"/.exec(resp.headers.get('Content-Disposition') || '')?.[1] || 'nineguard-export';
    const url = URL.createObjectURL(await resp.blob());
    const a = Object.assign(document.createElement('a'), { href: url, download: name });
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  },
};
```

- [ ] **Step 2: Reports Last Active**

In `web/static/js/views/reports.js`, change the `ui.js` import to:

```js
import { h, icon, emptyState, fmtNum, fmtCompact, fmtAgo, fmtDateTime, tzLabel, toast } from '../ui.js';
```

Insert before `  // ── Visual Token Ratio Bar Helper ──`:

```js
  // ── Last Active (all-time, not limited to the selected period) ──
  function renderLastActive(ts) {
    const ms = ts ? Date.parse(ts) : NaN;
    if (isNaN(ms)) return h('span', { class: 'muted', style: { fontSize: '11px' } }, 'Last active: Never');
    return h('span', {
      class: 'muted',
      style: { fontSize: '11px' },
      title: `${fmtDateTime(ms)} (${tzLabel()}) · all-time, any status`,
    }, `Last active: ${fmtAgo(ms)}`);
  }
```

Replace both lines of the form

```js
            h('span', { class: 'muted', style: { fontSize: '11px' } }, k.last_active_at ? `Last active: ${new Date(k.last_active_at).toLocaleString()}` : '')
```

(one uses `k.`, the other `m.`) with `renderLastActive(k.last_active_at)` and `renderLastActive(m.last_active_at)` respectively.

- [ ] **Step 3: Syntax check**

Run: `node --check web/static/js/api.js && node --check web/static/js/views/reports.js`
Expected: no output, exit 0.

- [ ] **Step 4: Manual check**

Run `go run ./cmd/nineguard` (or the Windows tray build), open DevTools → Network, open **Usage Reports**: requests to `/api/v1/traffic/report` include `tz=<your zone>`; expanded cards show `Last active: Nm ago` with the exact local time in the tooltip. Click **Export**: the request URL is `/api/v1/traffic?export=csv&period=...&tz=...` (single `?`).

- [ ] **Step 5: Commit**

```bash
git add web/static/js/api.js web/static/js/views/reports.js
git commit -m "fix(ui): send viewer tz; relative Last Active in Usage Reports"
```

---

### Task 8: Phase 1 verification and VM deploy (checkpoint)

- [ ] **Step 1: Full test suite, both build modes**

Run: `go vet ./... && go test ./... && CGO_ENABLED=0 go test -tags server ./...`
Expected: all `ok`.

- [ ] **Step 2: Formatting**

Run: `gofmt -l internal cmd`
Expected: no output (on Windows checkouts with CRLF, run `git diff --check` instead and ensure no whitespace errors in changed files).

- [ ] **Step 3: Push and deploy**

```bash
git push origin main
```

On the VM: `cd ~/nineguard && git pull && docker compose up -d --build`. Then check `docker compose logs --tail=50 nineguard` shows no errors.

- [ ] **Step 4: Production smoke check** (viewer in UTC+7)

Open Usage Reports → "Last Month": Last Active for a key used today shows "Nm ago", not last month's date. Dashboard → "Today" counts start at local midnight (send one request after 00:00 local and before 07:00 local; it must appear under Today).

**Stop here. Phase 2 starts only after Phase 1 is confirmed on the VM.**

---

# Phase 2 — Key Identity and Listing

### Task 9: `traffic_logs.api_key_id` migration and backfill

**Files:**
- Modify: `internal/db/db.go`, `cmd/nineguard/main.go`
- Test: `internal/db/migrate_key_id_test.go`

**Interfaces:**
- Produces: column `traffic_logs.api_key_id TEXT`; indexes `idx_traffic_key_id`, `idx_traffic_key_id_ts`; settings row `migration.traffic_key_id = done`; `var db.BackfilledTrafficKeyIDs int64`.

- [ ] **Step 1: Write the failing test** — create `internal/db/migrate_key_id_test.go`:

```go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func columnExists(t *testing.T, d *DB, table, col string) bool {
	t.Helper()
	rows, err := d.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		if name == col {
			return true
		}
	}
	return false
}

func indexExists(t *testing.T, d *DB, name string) bool {
	t.Helper()
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&n)
	return n == 1
}

func TestTrafficKeyIDColumnAndIndexes(t *testing.T) {
	d, err := InitDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !columnExists(t, d, "traffic_logs", "api_key_id") {
		t.Error("traffic_logs.api_key_id missing")
	}
	for _, idx := range []string{"idx_traffic_key_id", "idx_traffic_key_id_ts"} {
		if !indexExists(t, d, idx) {
			t.Errorf("index %s missing", idx)
		}
	}
	var v string
	if err := d.QueryRow("SELECT value FROM settings WHERE key = 'migration.traffic_key_id'").Scan(&v); err != nil || v != "done" {
		t.Errorf("guard row = %q, %v", v, err)
	}
}

func TestTrafficKeyIDBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-migration database: clear guard and links.
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec("DELETE FROM settings WHERE key = 'migration.traffic_key_id'")
	mustExec(`INSERT INTO api_keys (id, key, prefix, name) VALUES
		('k-pi',  'sk-ng-aaaaaaaaaaaa1111', 'sk-ng-aaaaaa', 'pi-dev'),
		('k-mar', 'sk-ng-bbbbbbbbbbbb2222', 'sk-ng-bbbbbb', 'marinara'),
		('k-d1',  'sk-ng-cccccccccccc3333', 'sk-ng-cccccc', 'dup'),
		('k-d2',  'sk-ng-dddddddddddd3333', 'sk-ng-dddddd', 'dup')`)
	ins := func(masked, name string) {
		mustExec(`INSERT INTO traffic_logs (api_key, api_key_name, model, status_code) VALUES (?, ?, 'm', 200)`, masked, name)
	}
	ins("sk-ng-...1111", "pi-dev")   // unique match -> k-pi
	ins("sk-ng-...1111", "pi-dev")   // unique match -> k-pi
	ins("sk-ng-...2222", "marinara") // unique match -> k-mar
	ins("sk-ng-...9999", "pi-dev")   // suffix mismatch -> NULL
	ins("sk-ng-...3333", "dup")      // two keys match -> NULL (ambiguous)
	ins("sk-ng-...1111", "old-name") // name mismatch -> NULL
	ins("unauthorized", "Unknown")   // 401 row -> NULL
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if BackfilledTrafficKeyIDs != 3 {
		t.Errorf("BackfilledTrafficKeyIDs = %d, want 3", BackfilledTrafficKeyIDs)
	}
	count := func(where string) int {
		var n int
		_ = d.QueryRow("SELECT COUNT(*) FROM traffic_logs WHERE " + where).Scan(&n)
		return n
	}
	if n := count("api_key_id = 'k-pi'"); n != 2 {
		t.Errorf("k-pi rows = %d, want 2", n)
	}
	if n := count("api_key_id = 'k-mar'"); n != 1 {
		t.Errorf("k-mar rows = %d, want 1", n)
	}
	if n := count("api_key_id IS NULL"); n != 4 {
		t.Errorf("unlinked rows = %d, want 4", n)
	}

	// Guard: a later run must not touch rows even if they would now match.
	mustExec2 := func(q string) {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mustExec2(`INSERT INTO traffic_logs (api_key, api_key_name, model, status_code) VALUES ('sk-ng-...2222', 'marinara', 'm', 200)`)
	d.Close()
	d2, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if BackfilledTrafficKeyIDs != 0 {
		t.Errorf("second run backfilled %d rows, want 0", BackfilledTrafficKeyIDs)
	}
	var id sql.NullString
	_ = d2.QueryRow("SELECT api_key_id FROM traffic_logs ORDER BY id DESC LIMIT 1").Scan(&id)
	if id.Valid {
		t.Errorf("guarded run linked a new row: %v", id.String)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -v`
Expected: FAIL — `undefined: BackfilledTrafficKeyIDs`.

- [ ] **Step 3: Implement**

In `migrate()`, directly after the line `_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_id)")`, add:

```go

	// Traffic linked to API keys by ID (ADR 0003).
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN api_key_id TEXT")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_id ON traffic_logs(api_key_id)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_id_ts ON traffic_logs(api_key_id, timestamp)")
	if err := d.backfillTrafficKeyID(); err != nil {
		return fmt.Errorf("backfill traffic_logs.api_key_id: %w", err)
	}
```

Append to `db.go`:

```go
// BackfilledTrafficKeyIDs is the number of traffic rows linked to a key by the
// one-time backfill during this process's InitDB (0 when the backfill already
// ran earlier). main logs it after the system log is ready.
var BackfilledTrafficKeyIDs int64

// backfillTrafficKeyID links historical traffic rows to API keys by matching
// key name plus the last 4 characters of the key. Rows with zero or several
// matching keys stay NULL. Runs once, guarded by a settings row.
func (d *DB) backfillTrafficKeyID() error {
	BackfilledTrafficKeyIDs = 0
	var done string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'migration.traffic_key_id'").Scan(&done)
	if err == nil && done == "done" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	res, err := d.Exec(`
		UPDATE traffic_logs
		SET api_key_id = (
			SELECT k.id FROM api_keys k
			WHERE k.name = traffic_logs.api_key_name
			  AND substr(k.key, -4) = substr(traffic_logs.api_key, -4)
		)
		WHERE api_key_id IS NULL
		  AND (
			SELECT COUNT(*) FROM api_keys k
			WHERE k.name = traffic_logs.api_key_name
			  AND substr(k.key, -4) = substr(traffic_logs.api_key, -4)
		  ) = 1
	`)
	if err != nil {
		return err
	}
	BackfilledTrafficKeyIDs, _ = res.RowsAffected()

	_, err = d.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES ('migration.traffic_key_id', 'done', CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = 'done', updated_at = CURRENT_TIMESTAMP
	`)
	return err
}
```

In `cmd/nineguard/main.go`, directly after `slog.SetDefault(slog.New(syslog.NewSlogHandler(syslogMgr, baseHandler)))`:

```go
	if n := db.BackfilledTrafficKeyIDs; n > 0 {
		slog.Info("linked historical traffic to API keys", "rows", n)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/db/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/db.go internal/db/migrate_key_id_test.go cmd/nineguard/main.go
git commit -m "feat(db): traffic_logs.api_key_id with indexes and one-time backfill"
```

---

### Task 10: Proxy records `api_key_id`

**Files:**
- Modify: `internal/traffic/traffic.go` (`LogEntry`, `Record`), `internal/proxy/proxy.go`
- Test: `internal/proxy/key_id_test.go`

**Interfaces:**
- Consumes: column from Task 9; `keys.KeyInfo.ID`.
- Produces: `traffic.LogEntry.APIKeyID string` (JSON `api_key_id,omitempty`).

- [ ] **Step 1: Write the failing test** — create `internal/proxy/key_id_test.go`:

```go
package proxy_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

// waitForRows polls until traffic_logs has n rows; the success path records asynchronously.
func waitForRows(t *testing.T, database *db.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var c int
		_ = database.QueryRow("SELECT COUNT(*) FROM traffic_logs").Scan(&c)
		if c >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d traffic rows", n)
}

func TestProxyRecordsAPIKeyID(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keyid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	km := keys.NewManager(database, "")
	mm := models.NewManager(database)
	pm := providers.NewManager(database)
	tm := traffic.NewManager(database)
	if _, err := pm.CreateProvider("Mock", upstream.URL, "", "mock", true, true); err != nil {
		t.Fatal(err)
	}
	// Nothing listens on port 1, so forwarding fails with 502.
	if _, err := pm.CreateProvider("Dead", "http://127.0.0.1:1", "", "dead", false, true); err != nil {
		t.Fatal(err)
	}
	key, err := km.CreateKey("pi-dev", "custom", nil, []string{"mock/ok", "dead/x"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := proxy.NewProxy(mm, tm, km, pm)
	if err != nil {
		t.Fatal(err)
	}

	send := func(model string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
			strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+key.RawKey)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("mock/ok"); code != http.StatusOK {
		t.Fatalf("success path: %d", code)
	}
	if code := send("mock/other"); code != http.StatusForbidden { // model_not_allowed
		t.Fatalf("403 path: %d", code)
	}
	if code := send("dead/x"); code != http.StatusBadGateway { // upstream unreachable
		t.Fatalf("502 path: %d", code)
	}
	_ = mm.SetModelEnabled("mock/ok", false)
	if code := send("mock/ok"); code != http.StatusForbidden { // model_disabled
		t.Fatalf("firewall path: %d", code)
	}
	waitForRows(t, database, 4)

	rows, err := database.Query("SELECT status_code, api_key_id FROM traffic_logs ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var code int
		var id sql.NullString
		_ = rows.Scan(&code, &id)
		if !id.Valid || id.String != key.ID {
			t.Errorf("row status %d: api_key_id = %v, want %s", code, id, key.ID)
		}
		n++
	}
	if n != 4 {
		t.Errorf("rows = %d, want 4", n)
	}

	// 401 rows carry no key ID.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mock/ok"}`))
	req.Header.Set("Authorization", "Bearer nope")
	p.ServeHTTP(httptest.NewRecorder(), req)
	var id sql.NullString
	_ = database.QueryRow("SELECT api_key_id FROM traffic_logs WHERE status_code = 401").Scan(&id)
	if id.Valid {
		t.Errorf("401 row has api_key_id %q", id.String)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run APIKeyID -v`
Expected: FAIL — `row status 200: api_key_id = { false}, want <id>` (×4).

- [ ] **Step 3: Implement**

`traffic.go` — in `LogEntry` after `APIKeyName`:

```go
	APIKeyID         string    `json:"api_key_id,omitempty"`
```

In `Record`, replace the `INSERT` statement and its argument list start with:

```go
	keyID := sql.NullString{String: entry.APIKeyID, Valid: entry.APIKeyID != ""}

	_, err := m.db.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_name, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens,
			duration_ms, status_code, client_ip, stream, error_message, level
		) VALUES (CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, maskedKey, entry.APIKeyName, keyID, entry.ProviderID, entry.Model, entry.PromptTokens, entry.CompletionTokens, entry.TotalTokens,
		entry.DurationMs, entry.StatusCode, entry.ClientIP, streamInt, errMsg, entry.Level)
```

`proxy.go` — in each of the four authenticated `p.traffic.Record(&traffic.LogEntry{...})` calls (model not allowed 403, model disabled 403, upstream 502, final response in the goroutine), add directly after the `APIKeyName: keyName,` line:

```go
			APIKeyID:     keyInfo.ID,
```

(align with surrounding fields; `gofmt` fixes spacing). Do **not** touch the 401 `Record` call (no key).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy/ ./internal/traffic/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/traffic/traffic.go internal/proxy/proxy.go internal/proxy/key_id_test.go
git commit -m "feat(proxy): record api_key_id on every authenticated request"
```

---

### Task 11: Key stats joined by `api_key_id`

**Files:**
- Modify: `internal/keys/keys.go` (replace `ListKeys`)
- Modify: `internal/keys/lastused_test.go` (insert `api_key_id`)
- Test: `internal/keys/list_test.go`

**Interfaces:**
- Produces (package `keys`, unexported, used by Task 12): `const keyStatsFrom`, `const keyStatsColumns`, `func (m *Manager) scanKeyRows(rows *sql.Rows) ([]KeyInfo, error)`.
- Test helpers (package `keys_test`, used by Task 12): `newKeysDB(t) (*db.DB, *keys.Manager)`, `addKey(t, database, id, name, mode string, active bool, created string)`, `addTraffic(t, database, keyID, ts string, status, tokens int)`, `ids([]keys.KeyInfo) []string`, `idsStr([]keys.KeyInfo) string`.

- [ ] **Step 1: Write the failing test** — create `internal/keys/list_test.go`:

```go
package keys_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func newKeysDB(t *testing.T) (*db.DB, *keys.Manager) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	km := keys.NewManager(database, "")
	// Drop the auto-created default key so tests control the full set.
	if _, err := database.Exec("DELETE FROM api_keys"); err != nil {
		t.Fatal(err)
	}
	return database, km
}

// addKey inserts a key with a fixed created_at so ordering is deterministic.
func addKey(t *testing.T, database *db.DB, id, name, mode string, active bool, created string) {
	t.Helper()
	act := 0
	if active {
		act = 1
	}
	_, err := database.Exec(`INSERT INTO api_keys (id, key, prefix, name, is_active, model_access_mode, created_at, updated_at)
		VALUES (?, ?, 'sk-ng-', ?, ?, ?, ?, ?)`, id, "sk-ng-"+id+"-raw-key-0000", name, act, mode, created, created)
	if err != nil {
		t.Fatal(err)
	}
}

func addTraffic(t *testing.T, database *db.DB, keyID, ts string, status, tokens int) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, status_code, total_tokens)
		VALUES (?, 'masked', 'whatever', ?, 'm', ?, ?)`, ts, keyID, status, tokens)
	if err != nil {
		t.Fatal(err)
	}
}

func TestListKeysStatsByKeyID(t *testing.T) {
	database, km := newKeysDB(t)
	addKey(t, database, "a", "pi-dev", "all", true, "2026-10-01 00:00:00")
	addKey(t, database, "b", "pi-dev", "all", true, "2026-10-02 00:00:00") // same name, separate stats
	addTraffic(t, database, "a", "2026-10-06 09:26:40", 403, 0)
	addTraffic(t, database, "a", "2026-10-05 09:00:00", 200, 50)

	list, err := km.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" {
		t.Fatalf("order = %v", ids(list))
	}
	a := list[1]
	if a.TotalRequests != 2 || a.TotalTokens != 50 {
		t.Errorf("a stats = %d req %d tok", a.TotalRequests, a.TotalTokens)
	}
	if a.LastUsedAt == nil || *a.LastUsedAt != "2026-10-06T09:26:40Z" {
		t.Errorf("a last_used_at = %v", a.LastUsedAt)
	}
	b := list[0]
	if b.TotalRequests != 0 || b.LastUsedAt != nil {
		t.Errorf("b should have no stats, got %d req, last %v", b.TotalRequests, b.LastUsedAt)
	}

	// Renaming keeps stats.
	if _, err := km.UpdateKey("a", "pi-dev-renamed", "all", nil, nil); err != nil {
		t.Fatal(err)
	}
	list, _ = km.ListKeys()
	for _, k := range list {
		if k.ID == "a" && k.TotalRequests != 2 {
			t.Errorf("renamed key lost stats: %d", k.TotalRequests)
		}
	}
}

func ids(list []keys.KeyInfo) []string {
	out := make([]string, len(list))
	for i, k := range list {
		out[i] = k.ID
	}
	return out
}

func idsStr(list []keys.KeyInfo) string { return fmt.Sprint(ids(list)) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keys/ -run ListKeysStats -v`
Expected: FAIL — `b should have no stats, got 2 req` (old `OR`/`LIKE` name join counts the same-name key's traffic).

- [ ] **Step 3: Implement**

Replace the whole `ListKeys` function (from its doc comment through its closing brace, right before `// ToggleKey activates or deactivates an API key`) with:

```go
// keyStatsFrom is the FROM clause shared by ListKeys and ListKeysPage: every
// key with its all-time traffic stats, linked by traffic_logs.api_key_id.
const keyStatsFrom = `
	FROM api_keys k
	LEFT JOIN (
		SELECT api_key_id,
		       COUNT(*) AS total_requests,
		       COALESCE(SUM(total_tokens), 0) AS total_tokens,
		       MAX(timestamp) AS last_used_at
		FROM traffic_logs
		WHERE api_key_id IS NOT NULL
		GROUP BY api_key_id
	) s ON s.api_key_id = k.id`

const keyStatsColumns = `
	k.id, k.key, k.prefix, k.name, k.is_active,
	COALESCE(k.model_access_mode, 'all'),
	COALESCE(k.model_group_ids, '[]'),
	COALESCE(k.allowed_models, ''),
	k.created_at, k.updated_at,
	COALESCE(s.total_requests, 0),
	COALESCE(s.total_tokens, 0),
	s.last_used_at`

func (m *Manager) scanKeyRows(rows *sql.Rows) ([]KeyInfo, error) {
	list := make([]KeyInfo, 0)
	for rows.Next() {
		var ki KeyInfo
		var rawKey string
		var isActiveInt int
		var mode, rawGroupIDs, rawModels string
		var lastUsed sql.NullString
		if err := rows.Scan(
			&ki.ID, &rawKey, &ki.Prefix, &ki.Name, &isActiveInt,
			&mode, &rawGroupIDs, &rawModels,
			&ki.CreatedAt, &ki.UpdatedAt,
			&ki.TotalRequests, &ki.TotalTokens, &lastUsed,
		); err != nil {
			return nil, err
		}
		ki.IsActive = (isActiveInt == 1)
		ki.Key = MaskKey(rawKey)
		ki.RawKey = rawKey
		ki.ModelAccessMode = mode
		ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
		ki.AllowedModels = ParseAllowedModels(rawModels)
		ki.manager = m
		ki.LastUsedAt = timeutil.NullTimeString(lastUsed)
		list = append(list, ki)
	}
	return list, rows.Err()
}

// ListKeys returns all NineGuard API keys with all-time request and token
// stats, newest first. Used by the legacy (unpaged) GET /api/v1/keys.
func (m *Manager) ListKeys() ([]KeyInfo, error) {
	rows, err := m.db.Query("SELECT " + keyStatsColumns + keyStatsFrom + " ORDER BY k.created_at DESC, k.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return m.scanKeyRows(rows)
}
```

In `internal/keys/lastused_test.go`, replace the traffic insert with the ID-linked version:

```go
	if _, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, status_code)
		VALUES ('2026-10-06 09:26:40', ?, 'pi-dev', ?, '9r/claude', 403)`, k.Key, k.ID); err != nil {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/keys/ -v`
Expected: PASS (including existing `TestKeyManagerCRUD`, `TestModelGroupDynamicLinking`).

- [ ] **Step 5: Commit**

```bash
git add internal/keys/keys.go internal/keys/list_test.go internal/keys/lastused_test.go
git commit -m "refactor(keys): join key stats on traffic_logs.api_key_id"
```

---

### Task 12: `ListKeysPage` — SQL paging, sorting, search, filters

**Files:**
- Create: `internal/keys/page.go`
- Test: `internal/keys/page_test.go`

**Interfaces:**
- Consumes: `keyStatsFrom`, `keyStatsColumns`, `scanKeyRows` (Task 11).
- Produces:
  - `type ListOptions struct{ Page, Limit int; Sort, Order, Query, Status, Mode string }`
  - `func (o *ListOptions) Normalize() error` (error text contains `allowed values are`)
  - `type KeyPage struct{ Keys []KeyInfo; Total, Page, Limit int }` (JSON `keys,total,page,limit`)
  - `func ClampLimit(n int) int`
  - `var AllowedLimits, SortFields, SortOrders, StatusFilters, ModeFilters`
  - `func (m *Manager) ListKeysPage(opts ListOptions) (*KeyPage, error)`

- [ ] **Step 1: Write the failing test** — create `internal/keys/page_test.go`:

```go
package keys_test

import (
	"strings"
	"testing"

	"nineguard/internal/keys"
)

// seedPaging creates 5 keys:
//
//	id  name      mode    active  created     requests tokens last_used
//	k1  Alpha     all     yes     10-01       3        300    10-06 09:00
//	k2  bravo     group   yes     10-02       1        900    10-06 12:00
//	k3  Charlie   custom  no      10-03       0        0      never
//	k4  delta     all     yes     10-04       0        0      never
//	k5  50%_off   group   no      10-05       2        100    10-01 00:00
func seedPaging(t *testing.T) *keys.Manager {
	t.Helper()
	database, km := newKeysDB(t)
	addKey(t, database, "k1", "Alpha", "all", true, "2026-10-01 00:00:00")
	addKey(t, database, "k2", "bravo", "group", true, "2026-10-02 00:00:00")
	addKey(t, database, "k3", "Charlie", "custom", false, "2026-10-03 00:00:00")
	addKey(t, database, "k4", "delta", "all", true, "2026-10-04 00:00:00")
	addKey(t, database, "k5", "50%_off", "group", false, "2026-10-05 00:00:00")
	addTraffic(t, database, "k1", "2026-10-06 09:00:00", 200, 100)
	addTraffic(t, database, "k1", "2026-10-05 09:00:00", 200, 100)
	addTraffic(t, database, "k1", "2026-10-04 09:00:00", 403, 100)
	addTraffic(t, database, "k2", "2026-10-06 12:00:00", 200, 900)
	addTraffic(t, database, "k5", "2026-10-01 00:00:00", 200, 50)
	addTraffic(t, database, "k5", "2026-09-30 00:00:00", 200, 50)
	return km
}

func page(t *testing.T, km *keys.Manager, o keys.ListOptions) *keys.KeyPage {
	t.Helper()
	p, err := km.ListKeysPage(o)
	if err != nil {
		t.Fatalf("ListKeysPage(%+v): %v", o, err)
	}
	return p
}

func TestListKeysPageSorting(t *testing.T) {
	km := seedPaging(t)
	cases := []struct {
		sort, order, want string
	}{
		// Never-used keys (k3, k4) last in both directions; tie-break created_at DESC.
		{"last_active", "desc", "[k2 k1 k5 k4 k3]"},
		{"last_active", "asc", "[k5 k1 k2 k4 k3]"},
		{"requests", "desc", "[k1 k5 k2 k4 k3]"},
		{"requests", "asc", "[k2 k5 k1 k4 k3]"},
		{"tokens", "desc", "[k2 k1 k5 k4 k3]"},
		{"tokens", "asc", "[k5 k1 k2 k4 k3]"},
		// Case-insensitive name sort.
		{"name", "asc", "[k5 k1 k2 k3 k4]"},
		{"name", "desc", "[k4 k3 k2 k1 k5]"},
		{"created", "asc", "[k1 k2 k3 k4 k5]"},
		{"created", "desc", "[k5 k4 k3 k2 k1]"},
		// Disabled (0) first ascending; ties by created_at DESC.
		{"status", "asc", "[k5 k3 k4 k2 k1]"},
		{"status", "desc", "[k4 k2 k1 k5 k3]"},
	}
	for _, c := range cases {
		p := page(t, km, keys.ListOptions{Page: 1, Limit: 25, Sort: c.sort, Order: c.order})
		if got := idsStr(p.Keys); got != c.want {
			t.Errorf("sort=%s order=%s: got %s, want %s", c.sort, c.order, got, c.want)
		}
		if p.Total != 5 {
			t.Errorf("total = %d", p.Total)
		}
	}
}

func TestListKeysPageDefaultsAndPaging(t *testing.T) {
	km := seedPaging(t)
	p := page(t, km, keys.ListOptions{})
	if p.Page != 1 || p.Limit != 25 || idsStr(p.Keys) != "[k2 k1 k5 k4 k3]" {
		t.Errorf("defaults: page %d limit %d keys %s", p.Page, p.Limit, idsStr(p.Keys))
	}

	p = page(t, km, keys.ListOptions{Page: 1, Limit: 10, Sort: "created", Order: "asc"})
	if len(p.Keys) != 5 {
		t.Errorf("limit 10 page 1: %d keys", len(p.Keys))
	}
	p = page(t, km, keys.ListOptions{Page: 2, Limit: 10})
	if len(p.Keys) != 0 || p.Total != 5 || p.Page != 2 {
		t.Errorf("beyond last page: %d keys total %d page %d", len(p.Keys), p.Total, p.Page)
	}
	p = page(t, km, keys.ListOptions{Page: -3})
	if p.Page != 1 {
		t.Errorf("negative page normalized to %d", p.Page)
	}
}

func TestClampLimit(t *testing.T) {
	cases := map[int]int{0: 25, -1: 25, 1: 10, 10: 10, 17: 10, 18: 25, 25: 25, 37: 25, 38: 50, 74: 50, 76: 100, 500: 100}
	for in, want := range cases {
		if got := keys.ClampLimit(in); got != want {
			t.Errorf("ClampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestListKeysPageFilters(t *testing.T) {
	km := seedPaging(t)
	cases := []struct {
		opts keys.ListOptions
		want string
	}{
		{keys.ListOptions{Query: "AL", Sort: "created", Order: "asc"}, "[k1]"}, // case-insensitive substring
		{keys.ListOptions{Query: "%", Sort: "created", Order: "asc"}, "[k5]"},  // % matched literally
		{keys.ListOptions{Query: "_", Sort: "created", Order: "asc"}, "[k5]"},  // _ matched literally
		{keys.ListOptions{Status: "active", Sort: "created", Order: "asc"}, "[k1 k2 k4]"},
		{keys.ListOptions{Status: "disabled", Sort: "created", Order: "asc"}, "[k3 k5]"},
		{keys.ListOptions{Mode: "group", Sort: "created", Order: "asc"}, "[k2 k5]"},
		{keys.ListOptions{Mode: "all", Status: "active", Sort: "created", Order: "asc"}, "[k1 k4]"},
		{keys.ListOptions{Query: "zzz"}, "[]"},
	}
	for _, c := range cases {
		p := page(t, km, c.opts)
		if got := idsStr(p.Keys); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.opts, got, c.want)
		}
		if p.Total != len(p.Keys) {
			t.Errorf("%+v: total %d != len %d", c.opts, p.Total, len(p.Keys))
		}
	}
}

func TestListKeysPageInvalidParams(t *testing.T) {
	km := seedPaging(t)
	for _, o := range []keys.ListOptions{
		{Sort: "bogus"}, {Order: "sideways"}, {Status: "maybe"}, {Mode: "partial"},
	} {
		_, err := km.ListKeysPage(o)
		if err == nil || !strings.Contains(err.Error(), "allowed values are") {
			t.Errorf("%+v: err = %v", o, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keys/ -run 'Page|ClampLimit' -v`
Expected: FAIL — `undefined: keys.ListOptions`, `undefined: keys.ClampLimit`.

- [ ] **Step 3: Implement** — create `internal/keys/page.go`:

```go
package keys

import (
	"fmt"
	"strings"
)

// ListOptions controls paging, sorting, and filtering of GET /api/v1/keys?page=.
type ListOptions struct {
	Page   int    // 1-based
	Limit  int    // one of AllowedLimits
	Sort   string // one of SortFields
	Order  string // "asc" | "desc"
	Query  string // case-insensitive substring of key name
	Status string // "all" | "active" | "disabled"
	Mode   string // "any" | "all" | "group" | "custom"
}

// KeyPage is one page of keys plus the total number of keys matching the filters.
type KeyPage struct {
	Keys  []KeyInfo `json:"keys"`
	Total int       `json:"total"`
	Page  int       `json:"page"`
	Limit int       `json:"limit"`
}

// Allowed values, exported so the handler can list them in 400 messages.
var (
	AllowedLimits = []int{10, 25, 50, 100}
	SortFields    = []string{"name", "status", "created", "last_active", "requests", "tokens"}
	SortOrders    = []string{"asc", "desc"}
	StatusFilters = []string{"all", "active", "disabled"}
	ModeFilters   = []string{"any", "all", "group", "custom"}
)

// sortColumns maps a sort field to its SQL expression. usedFlag is true for
// fields where never-used keys must sort last regardless of order.
var sortColumns = map[string]struct {
	expr     string
	usedLast bool
}{
	"name":        {"k.name COLLATE NOCASE", false},
	"status":      {"k.is_active", false},
	"created":     {"k.created_at", false},
	"last_active": {"s.last_used_at", true},
	"requests":    {"COALESCE(s.total_requests, 0)", true},
	"tokens":      {"COALESCE(s.total_tokens, 0)", true},
}

// ClampLimit returns the allowed page size nearest to n (ties go to the smaller).
// Zero or negative values return the default, 25.
func ClampLimit(n int) int {
	if n <= 0 {
		return 25
	}
	best := AllowedLimits[0]
	for _, l := range AllowedLimits {
		if abs(n-l) < abs(n-best) {
			best = l
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Normalize fills defaults and validates every field. It returns an error
// naming the invalid parameter and its allowed values.
func (o *ListOptions) Normalize() error {
	if o.Page < 1 {
		o.Page = 1
	}
	o.Limit = ClampLimit(o.Limit)
	defaults := []struct {
		name    string
		val     *string
		def     string
		allowed []string
	}{
		{"sort", &o.Sort, "last_active", SortFields},
		{"order", &o.Order, "desc", SortOrders},
		{"status", &o.Status, "all", StatusFilters},
		{"mode", &o.Mode, "any", ModeFilters},
	}
	for _, d := range defaults {
		*d.val = strings.ToLower(strings.TrimSpace(*d.val))
		if *d.val == "" {
			*d.val = d.def
		}
		if !oneOf(*d.val, d.allowed) {
			return fmt.Errorf("invalid %s %q: allowed values are %s", d.name, *d.val, strings.Join(d.allowed, ", "))
		}
	}
	o.Query = strings.TrimSpace(o.Query)
	return nil
}

// likeEscape escapes LIKE wildcards so user input matches literally (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListKeysPage returns one page of keys with all-time stats. Sorting, filtering
// and paging happen in SQL. Never-used keys sort last for last_active,
// requests and tokens in both directions; ties break by created_at DESC, id.
func (m *Manager) ListKeysPage(opts ListOptions) (*KeyPage, error) {
	if err := opts.Normalize(); err != nil {
		return nil, err
	}

	var conds []string
	var args []any
	if opts.Query != "" {
		conds = append(conds, `k.name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(opts.Query)+"%")
	}
	switch opts.Status {
	case "active":
		conds = append(conds, "k.is_active = 1")
	case "disabled":
		conds = append(conds, "k.is_active = 0")
	}
	if opts.Mode != "any" {
		conds = append(conds, "COALESCE(NULLIF(k.model_access_mode, ''), 'all') = ?")
		args = append(args, opts.Mode)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM api_keys k"+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	col := sortColumns[opts.Sort]
	dir := "ASC"
	if opts.Order == "desc" {
		dir = "DESC"
	}
	order := ""
	if col.usedLast {
		order = "(s.api_key_id IS NULL) ASC, "
	}
	order += fmt.Sprintf("%s %s, k.created_at DESC, k.id ASC", col.expr, dir)

	query := "SELECT " + keyStatsColumns + keyStatsFrom + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	pageArgs := append(append([]any{}, args...), opts.Limit, (opts.Page-1)*opts.Limit)
	rows, err := m.db.Query(query, pageArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := m.scanKeyRows(rows)
	if err != nil {
		return nil, err
	}
	return &KeyPage{Keys: list, Total: total, Page: opts.Page, Limit: opts.Limit}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/keys/ -v`
Expected: PASS — every sort/order row of `TestListKeysPageSorting`, plus filters (including literal `%`/`_`), paging beyond last page, invalid params.

- [ ] **Step 5: Commit**

```bash
git add internal/keys/page.go internal/keys/page_test.go
git commit -m "feat(keys): server-side paging, sorting, search and filters"
```

---

### Task 13: `GET /api/v1/keys` paged mode

**Files:**
- Modify: `internal/handler/handler.go` (replace `ListKeys`)
- Test: `internal/handler/keys_list_test.go`

**Interfaces:**
- Consumes: `keys.ListOptions`, `Normalize`, `ListKeysPage`, `KeyPage` (Task 12).

- [ ] **Step 1: Write the failing test** — create `internal/handler/keys_list_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
)

func TestListKeysLegacyAndPaged(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	km := keys.NewManager(database, "") // creates "Default Agent Key"
	for _, n := range []string{"pi-dev", "marinara"} {
		if _, err := km.CreateKey(n, "all", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	h := handler.New(nil, nil, nil, nil, km, nil, nil, "")

	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys"+query, nil)
		w := httptest.NewRecorder()
		h.ListKeys(w, req)
		return w
	}

	// Legacy: no page -> {"keys": [...]} only, all keys.
	w := get("")
	var legacy map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &legacy)
	if _, hasTotal := legacy["total"]; hasTotal || w.Code != http.StatusOK {
		t.Errorf("legacy response changed: %d %s", w.Code, w.Body.String())
	}
	var all []keys.KeyInfo
	_ = json.Unmarshal(legacy["keys"], &all)
	if len(all) != 3 {
		t.Errorf("legacy keys = %d, want 3", len(all))
	}

	// Paged.
	w = get("?page=1&limit=10&sort=name&order=asc&q=a")
	if w.Code != http.StatusOK {
		t.Fatalf("paged: %d %s", w.Code, w.Body.String())
	}
	var pg keys.KeyPage
	_ = json.Unmarshal(w.Body.Bytes(), &pg)
	if pg.Total != 2 || pg.Page != 1 || pg.Limit != 10 || len(pg.Keys) != 2 || pg.Keys[0].Name != "Default Agent Key" || pg.Keys[1].Name != "marinara" {
		t.Errorf("paged = %+v", pg)
	}

	// Limit clamped.
	w = get("?page=1&limit=7")
	_ = json.Unmarshal(w.Body.Bytes(), &pg)
	if pg.Limit != 10 {
		t.Errorf("limit 7 clamped to %d, want 10", pg.Limit)
	}

	// Invalid params -> 400 listing allowed values.
	for _, q := range []string{"?page=0", "?page=x", "?page=1&limit=x", "?page=1&sort=bogus", "?page=1&order=up", "?page=1&status=x", "?page=1&mode=x"} {
		w = get(q)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
	w = get("?page=1&sort=bogus")
	if !strings.Contains(w.Body.String(), "last_active") {
		t.Errorf("400 body should list allowed sorts: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run ListKeys -v`
Expected: FAIL — `paged = {Keys:[] Total:0 ...}` (handler ignores `page`), and invalid params return 200.

- [ ] **Step 3: Implement** — replace `func (h *Handler) ListKeys` (and its preceding comment, if any) with:

```go
// ListKeys serves GET /api/v1/keys. Without "page" it returns every key
// ({"keys": [...]}, legacy shape used by dropdowns and scripts). With "page"
// it returns one page: {"keys", "total", "page", "limit"}.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": []keys.KeyInfo{}})
		return
	}
	q := r.URL.Query()
	if !q.Has("page") {
		list, err := h.keys.ListKeys()
		if err != nil {
			slog.Error("failed to list keys", "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to retrieve API keys")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": list})
		return
	}

	pageNum, err := strconv.Atoi(q.Get("page"))
	if err != nil || pageNum < 1 {
		jsonError(w, http.StatusBadRequest, "invalid page: must be an integer >= 1")
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid limit: must be an integer")
			return
		}
	}
	opts := keys.ListOptions{
		Page:   pageNum,
		Limit:  limit,
		Sort:   q.Get("sort"),
		Order:  q.Get("order"),
		Query:  q.Get("q"),
		Status: q.Get("status"),
		Mode:   q.Get("mode"),
	}
	if err := opts.Normalize(); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	pageRes, err := h.keys.ListKeysPage(opts)
	if err != nil {
		slog.Error("failed to list keys page", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve API keys")
		return
	}
	jsonResponse(w, http.StatusOK, pageRes)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/handler.go internal/handler/keys_list_test.go
git commit -m "feat(api): paged GET /api/v1/keys with legacy fallback"
```

---

### Task 14: Reports, dashboard and traffic filter grouped by key ID

**Files:**
- Modify: `internal/traffic/traffic.go`, `internal/handler/handler.go` (`parseFilterParams`)
- Test: `internal/traffic/keyid_report_test.go`

**Interfaces:**
- Consumes: `api_key_id` (Tasks 9–10); `fixNow` (Task 2).
- Produces:
  - JSON fields: `KeyUsageBreakdown.key_id`, `.unlinked`; `KeyUsageSummary.key_id`, `.unlinked`; `KeyStat.key_id`, `.unlinked`; `KeyUsageTrendPoint.key_id`; `LogEntry.api_key_id` in `/api/v1/traffic`.
  - `traffic.FilterParams.APIKeyID string`; query param `key_id` on `/api/v1/traffic`, `/traffic/volume`, `/traffic/export`.
  - Unexported: `keyJoin`, `keyGroupExpr`, `keyNameExpr`, `splitKeyGroup(g string) (string, bool)`; `lastActiveBy(groupExpr, join string)`.

- [ ] **Step 1: Write the failing test** — create `internal/traffic/keyid_report_test.go`:

```go
package traffic

import (
	"testing"
)

func insertKeyed(t *testing.T, mgr *Manager, ts, keyID, keyName, masked, model string, status, tokens int) {
	t.Helper()
	var id any
	if keyID != "" {
		id = keyID
	}
	_, err := mgr.db.Exec(`
		INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, total_tokens, status_code, client_ip, level)
		VALUES (?, ?, ?, ?, ?, ?, ?, '127.0.0.1', '')`, ts, masked, keyName, id, model, tokens, status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUsageReportGroupsByKeyID(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")

	if _, err := mgr.db.Exec(`INSERT INTO api_keys (id, key, prefix, name) VALUES
		('k-pi', 'sk-ng-aaaa1111', 'sk-ng-', 'pi-dev-renamed'),
		('k-twin', 'sk-ng-bbbb2222', 'sk-ng-', 'twin')`); err != nil {
		t.Fatal(err)
	}
	// Linked: old name in traffic, current name from api_keys.
	insertKeyed(t, mgr, "2026-10-06 09:00:00", "k-pi", "pi-dev", "sk-ng-...1111", "m1", 200, 100)
	insertKeyed(t, mgr, "2026-10-06 09:30:00", "k-pi", "pi-dev-renamed", "sk-ng-...1111", "m2", 200, 50)
	// Same historical name, different linked key: must stay separate.
	insertKeyed(t, mgr, "2026-10-06 08:00:00", "k-twin", "pi-dev", "sk-ng-...2222", "m1", 200, 10)
	// Unlinked (NULL) and deleted-key rows group by historical name.
	insertKeyed(t, mgr, "2026-10-06 07:00:00", "", "legacy-bot", "sk-ng-...9999", "m1", 200, 7)
	insertKeyed(t, mgr, "2026-10-06 07:30:00", "k-deleted", "legacy-bot", "sk-ng-...8888", "m1", 403, 0)

	report, err := mgr.GetUsageReports("today", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]KeyUsageBreakdown{}
	for _, k := range report.KeysBreakdown {
		byName[k.KeyName] = k
	}
	if len(report.KeysBreakdown) != 3 {
		t.Fatalf("groups = %d, want 3: %+v", len(report.KeysBreakdown), report.KeysBreakdown)
	}
	pi := byName["pi-dev-renamed"]
	if pi.KeyID != "k-pi" || pi.Unlinked || pi.TotalRequests != 2 || pi.TotalTokens != 150 || len(pi.ModelUsage) != 2 {
		t.Errorf("pi group = %+v", pi)
	}
	if pi.LastActiveAt == nil || *pi.LastActiveAt != "2026-10-06T09:30:00Z" {
		t.Errorf("pi last active = %v", pi.LastActiveAt)
	}
	twin := byName["twin"]
	if twin.KeyID != "k-twin" || twin.TotalRequests != 1 {
		t.Errorf("twin group = %+v", twin)
	}
	legacy := byName["legacy-bot"]
	if !legacy.Unlinked || legacy.KeyID != "" || legacy.TotalRequests != 2 || legacy.BlockedRequests != 1 {
		t.Errorf("unlinked group = %+v", legacy)
	}
	if legacy.LastActiveAt == nil || *legacy.LastActiveAt != "2026-10-06T07:30:00Z" {
		t.Errorf("unlinked last active = %v", legacy.LastActiveAt)
	}

	// Model consumers carry key IDs.
	for _, mb := range report.ModelsBreakdown {
		if mb.Model != "m1" {
			continue
		}
		ids := map[string]bool{}
		for _, c := range mb.KeyConsumers {
			ids[c.KeyID] = true
			if c.KeyID == "" && !c.Unlinked {
				t.Errorf("consumer without ID must be unlinked: %+v", c)
			}
		}
		if !ids["k-pi"] || !ids["k-twin"] || !ids[""] {
			t.Errorf("m1 consumers = %+v", mb.KeyConsumers)
		}
	}

	stats, err := mgr.GetDashboardStats("today", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveKeys != 3 {
		t.Errorf("active keys = %d, want 3", stats.ActiveKeys)
	}
	if len(stats.TopKeys) != 3 || stats.TopKeys[0].KeyID != "k-pi" || stats.TopKeys[0].Name != "pi-dev-renamed" {
		t.Errorf("top keys = %+v", stats.TopKeys)
	}
	if stats.TopKeys[0].Trend == nil || sum(stats.TopKeys[0].Trend) != 2 {
		t.Errorf("pi trend = %v", stats.TopKeys[0].Trend)
	}
	found := false
	for _, p := range stats.KeyUsageTrends {
		if p.KeyID == "k-pi" && p.KeyName == "pi-dev-renamed" {
			found = true
		}
	}
	if !found {
		t.Errorf("key usage trends missing k-pi: %+v", stats.KeyUsageTrends)
	}

	// Traffic filter by key ID.
	logs, total, err := mgr.QueryLogs(FilterParams{Period: "all", APIKeyID: "k-pi"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || logs[0].APIKeyID != "k-pi" {
		t.Errorf("key_id filter: total %d", total)
	}
	vol, err := mgr.GetVolume(FilterParams{Period: "today", APIKeyID: "k-twin"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if vol.Totals["2xx"] != 1 {
		t.Errorf("volume key_id filter: %v", vol.Totals)
	}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/traffic/ -run GroupsByKeyID -v`
Expected: FAIL — build errors `unknown field KeyID`, `unknown field APIKeyID in struct literal of type FilterParams`.

- [ ] **Step 3: Types and shared grouping SQL**

Add fields (first in each struct):

```go
type KeyStat struct {
	KeyID    string  `json:"key_id,omitempty"` // empty for unlinked / deleted keys
	Unlinked bool    `json:"unlinked"`
```

```go
type KeyUsageTrendPoint struct {
	Time     string `json:"time"`
	KeyID    string `json:"key_id,omitempty"`
```

```go
type KeyUsageSummary struct {
	KeyID        string  `json:"key_id,omitempty"`
	Unlinked     bool    `json:"unlinked"`
```

```go
type KeyUsageBreakdown struct {
	KeyID            string              `json:"key_id,omitempty"` // empty for unlinked / deleted keys
	Unlinked         bool                `json:"unlinked"`         // true: grouped by historical key name
```

In `FilterParams` after `Cursor`:

```go
	APIKeyID  string         // exact traffic_logs.api_key_id match
```

Insert before `type Manager struct`:

```go
// Key grouping (ADR 0003). Traffic rows are grouped by API key ID when the
// key still exists; otherwise (NULL api_key_id, or the key was deleted) they
// fall into "unlinked" groups by historical key name. The expressions require
// traffic_logs aliased as t with keyJoin applied.
const (
	keyJoin      = " LEFT JOIN api_keys k ON k.id = t.api_key_id"
	keyGroupExpr = "CASE WHEN k.id IS NOT NULL THEN 'id:' || k.id ELSE 'name:' || COALESCE(NULLIF(t.api_key_name, ''), t.api_key, '') END"
	keyNameExpr  = "CASE WHEN k.id IS NOT NULL THEN k.name ELSE COALESCE(NULLIF(t.api_key_name, ''), t.api_key, '') END"
)

// splitKeyGroup turns a keyGroupExpr value into (key ID, unlinked).
func splitKeyGroup(g string) (string, bool) {
	if strings.HasPrefix(g, "id:") {
		return strings.TrimPrefix(g, "id:"), false
	}
	return "", true
}
```

- [ ] **Step 4: `key_id` filter in `QueryLogs` and `GetVolume`**

In **both** functions, directly after the `if p.APIKey != "" { ... }` block, add:

```go
	if p.APIKeyID != "" {
		conditions = append(conditions, "api_key_id = ?")
		args = append(args, p.APIKeyID)
	}
```

In `QueryLogs`' row query, change `COALESCE(api_key_name, ''), COALESCE(provider_id, '')` to `COALESCE(api_key_name, ''), COALESCE(api_key_id, ''), COALESCE(provider_id, '')` and the scan `&e.APIKeyName, &e.ProviderID` to `&e.APIKeyName, &e.APIKeyID, &e.ProviderID`.

In `internal/handler/handler.go` `parseFilterParams`, after `APIKey:    apiKey,` add `APIKeyID:  q.Get("key_id"),`.

- [ ] **Step 5: Dashboard**

In the dashboard `aggQuery`, remove the trailing `, COUNT(DISTINCT api_key)` column (so the SELECT ends with `COUNT(DISTINCT model)`) and remove `&stats.ActiveKeys,` from its `Scan`. Right after that `Scan`'s `if err != nil { return nil, err }`, add:

```go

	// Active keys: distinct key groups (key ID, or historical name when unlinked).
	_ = m.db.QueryRow(fmt.Sprintf(`SELECT COUNT(DISTINCT %s) FROM traffic_logs t%s WHERE %s`,
		keyGroupExpr, keyJoin, dateFilter), dateFilterArgs...).Scan(&stats.ActiveKeys)
```

Replace the block from `	// Top API Keys` up to (not including) `	// Top Error Sources` with:

```go
	// Top API Keys, grouped by key ID (unlinked rows by historical name).
	tSlotExpr := strings.ReplaceAll(slotExpr, "timestamp", "t.timestamp")
	topKeysQuery := fmt.Sprintf(`
		SELECT %s AS g, %s AS key_name, MAX(t.api_key), COUNT(*) AS reqs, COALESCE(SUM(t.total_tokens), 0) AS toks
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g
		ORDER BY toks DESC, reqs DESC
		LIMIT 8
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)
	keyIdx := make(map[string]int)
	rowsKeys, err := m.db.Query(topKeysQuery, dateFilterArgs...)
	if err == nil {
		for rowsKeys.Next() {
			var ks KeyStat
			var g string
			var masked sql.NullString
			if err := rowsKeys.Scan(&g, &ks.Name, &masked, &ks.Requests, &ks.Tokens); err == nil {
				ks.KeyID, ks.Unlinked = splitKeyGroup(g)
				ks.Key = masked.String
				if stats.TotalTokens > 0 {
					ks.Share = float64(ks.Tokens) / float64(stats.TotalTokens) * 100
				}
				ks.Trend = make([]int, len(timeSlots))
				keyIdx[g] = len(stats.TopKeys)
				stats.TopKeys = append(stats.TopKeys, ks)
			}
		}
		rowsKeys.Close()
	}

	if len(stats.TopKeys) > 0 {
		trendKeyQ := fmt.Sprintf(`
			SELECT %s AS g, %s AS ts, COUNT(*)
			FROM traffic_logs t%s
			WHERE %s
			GROUP BY g, ts
		`, keyGroupExpr, tSlotExpr, keyJoin, dateFilter)
		if rTr, errTr := m.db.Query(trendKeyQ, dateFilterArgs...); errTr == nil {
			for rTr.Next() {
				var g, ts string
				var cnt int
				if err := rTr.Scan(&g, &ts, &cnt); err == nil {
					if kIdx, ok := keyIdx[g]; ok {
						if tIdx, ok2 := slotIdx[ts]; ok2 {
							stats.TopKeys[kIdx].Trend[tIdx] = cnt
						}
					}
				}
			}
			rTr.Close()
		}
	}
```

Replace the block from `	// Query API Key usage breakdown over date/time slots` up to (not including) `	return stats, nil` with:

```go
	// Query API Key usage breakdown over date/time slots
	keySeriesQuery := fmt.Sprintf(`
		SELECT
			%s AS time_slot,
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			COUNT(*) AS reqs,
			COALESCE(SUM(t.total_tokens), 0) AS toks
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY time_slot, g
		ORDER BY time_slot ASC
	`, tSlotExpr, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	rowsKeySeries, err := m.db.Query(keySeriesQuery, dateFilterArgs...)
	if err == nil {
		defer rowsKeySeries.Close()
		for rowsKeySeries.Next() {
			var kp KeyUsageTrendPoint
			var g string
			var masked sql.NullString
			if err := rowsKeySeries.Scan(&kp.Time, &g, &kp.KeyName, &masked, &kp.Requests, &kp.Tokens); err == nil {
				kp.KeyID, _ = splitKeyGroup(g)
				kp.Key = masked.String
				stats.KeyUsageTrends = append(stats.KeyUsageTrends, kp)
			}
		}
	}
```

- [ ] **Step 6: Usage report**

In `GetUsageReports`, replace the block from `	// 2. Query Key-Model Cross Breakdown` up to (not including) `	// 5. Query Grouping per Model` with:

```go
	// 2. Query Key-Model Cross Breakdown, grouped by key ID (unlinked rows by historical name).
	keyModelMap := make(map[string][]ModelUsageSummary)
	modelKeyMap := make(map[string][]KeyUsageSummary)

	crossQuery := fmt.Sprintf(`
		SELECT
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			t.model,
			COALESCE(SUM(t.total_tokens), 0) AS tot_toks,
			COALESCE(SUM(t.prompt_tokens), 0) AS p_toks,
			COALESCE(SUM(t.completion_tokens), 0) AS c_toks,
			COUNT(*) AS reqs
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g, t.model
		ORDER BY tot_toks DESC
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	crossRows, err := m.db.Query(crossQuery, dateFilterArgs...)
	if err == nil {
		defer crossRows.Close()
		for crossRows.Next() {
			var g, kn, mod string
			var masked sql.NullString
			var tot, pt, ct, reqs int
			if err := crossRows.Scan(&g, &kn, &masked, &mod, &tot, &pt, &ct, &reqs); err == nil {
				keyID, unlinked := splitKeyGroup(g)
				keyModelMap[g] = append(keyModelMap[g], ModelUsageSummary{
					Model:        mod,
					TotalTokens:  tot,
					PromptTokens: pt,
					CompTokens:   ct,
					Requests:     reqs,
				})
				modelKeyMap[mod] = append(modelKeyMap[mod], KeyUsageSummary{
					KeyID:        keyID,
					Unlinked:     unlinked,
					KeyName:      kn,
					Key:          masked.String,
					TotalTokens:  tot,
					PromptTokens: pt,
					CompTokens:   ct,
					Requests:     reqs,
				})
			}
		}
	}

	// 3. Last Active per key and per model: all-time, any status (not period-bound).
	keyLastActive := m.lastActiveBy(keyGroupExpr, keyJoin)
	modelLastActive := m.lastActiveBy("t.model", "")

	// 4. Query Grouping per API Key
	keysQuery := fmt.Sprintf(`
		SELECT
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			COALESCE(SUM(t.total_tokens), 0) AS tot_toks,
			COALESCE(SUM(t.prompt_tokens), 0) AS p_toks,
			COALESCE(SUM(t.completion_tokens), 0) AS c_toks,
			COUNT(*) AS tot_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 200 AND t.status_code < 400 THEN 1 ELSE 0 END), 0) AS ok_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 400 AND t.status_code != 403 THEN 1 ELSE 0 END), 0) AS err_reqs,
			COALESCE(SUM(CASE WHEN t.status_code = 403 THEN 1 ELSE 0 END), 0) AS blk_reqs,
			COALESCE(ROUND(AVG(t.duration_ms)), 0) AS avg_dur
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g
		ORDER BY tot_toks DESC, tot_reqs DESC
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	kRows, err := m.db.Query(keysQuery, dateFilterArgs...)
	if err == nil {
		defer kRows.Close()
		for kRows.Next() {
			var kb KeyUsageBreakdown
			var g string
			var masked sql.NullString
			if err := kRows.Scan(
				&g, &kb.KeyName, &masked, &kb.TotalTokens, &kb.PromptTokens, &kb.CompletionTokens,
				&kb.TotalRequests, &kb.SuccessRequests, &kb.ErrorRequests, &kb.BlockedRequests,
				&kb.AvgDurationMs,
			); err == nil {
				kb.KeyID, kb.Unlinked = splitKeyGroup(g)
				kb.Key = masked.String
				if report.TotalTokens > 0 {
					kb.TokenShare = float64(kb.TotalTokens) / float64(report.TotalTokens) * 100
				}
				kb.LastActiveAt = keyLastActive[g]
				modelsUsed := keyModelMap[g]
				for i := range modelsUsed {
					if kb.TotalTokens > 0 {
						modelsUsed[i].Share = float64(modelsUsed[i].TotalTokens) / float64(kb.TotalTokens) * 100
					}
				}
				kb.ModelUsage = modelsUsed
				report.KeysBreakdown = append(report.KeysBreakdown, kb)
			}
		}
	}
```

Replace the whole `lastActiveBy` function from Task 3 with:

```go
// lastActiveBy returns MAX(timestamp) over all traffic (no period filter, all
// status codes) grouped by groupExpr, normalized to RFC 3339 UTC. traffic_logs
// is aliased t and join is appended after it. Both arguments must be trusted
// constants, never user input.
func (m *Manager) lastActiveBy(groupExpr, join string) map[string]*string {
	out := make(map[string]*string)
	rows, err := m.db.Query(fmt.Sprintf(
		"SELECT %s AS g, MAX(t.timestamp) FROM traffic_logs t%s GROUP BY g", groupExpr, join))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g sql.NullString
		var ts sql.NullString
		if err := rows.Scan(&g, &ts); err == nil && g.Valid {
			out[g.String] = timeutil.NullTimeString(ts)
		}
	}
	return out
}

// periodVolumeWindow converts a period into the [from, to] span used by volume
// histograms, with calendar boundaries in loc. "today" spans the whole local
// day so the chart has a stable width; open-ended windows end at to.
func periodVolumeWindow(period, startDate, endDate string, loc *time.Location, to time.Time) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	cur, _ := timeutil.ResolvePeriod(period, startDate, endDate, loc, nowFunc())
	from := cur.From
	if !cur.To.IsZero() {
		to = cur.To.Add(-time.Second)
	} else if !from.IsZero() && (period == "" || period == "today") && startDate == "" && endDate == "" {
		to = from.In(loc).AddDate(0, 0, 1).UTC().Add(-time.Second)
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go vet ./... && go test ./... `
Expected: all `ok` — notably `TestUsageReportGroupsByKeyID`, `TestUsageReportLastActiveIgnoresPeriod` (Task 3, still passing with the unlinked fallback).

- [ ] **Step 8: Commit**

```bash
git add internal/traffic/traffic.go internal/traffic/keyid_report_test.go internal/handler/handler.go
git commit -m "feat(reports): group per-key stats by api_key_id with unlinked fallback"
```

---

### Task 15: Key table state helpers (`keylist.js`)

**Files:**
- Create: `web/static/js/keylist.js`
- Test: `web/jstest/keylist.test.mjs`

**Interfaces:**
- Produces (ES module exports): `KEY_LIST_DEFAULTS`, `PAGE_SIZES`, `SORTABLE`, `stateFromParams(params)`, `paramsFromState(state)`, `apiQuery(state)`, `nextSort(state, field)`, `withFilter(state, changes)`, `totalPages(total, limit)`, `rangeLabel(page, limit, total, shown)`, `pageItems(page, pages)` (numbers and `'…'`), `pageAfterReload(page, shown)`.
- State shape: `{ page: number, limit: number, sort: string, order: 'asc'|'desc', q: string, status: string, mode: string }`.

- [ ] **Step 1: Write the failing test** — create `web/jstest/keylist.test.mjs`:

```js
// Run: node --test web/jstest/
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  KEY_LIST_DEFAULTS, stateFromParams, paramsFromState, apiQuery, nextSort,
  withFilter, totalPages, rangeLabel, pageItems, pageAfterReload,
} from '../static/js/keylist.js';

test('stateFromParams defaults and validation', () => {
  assert.deepEqual(stateFromParams({}), { ...KEY_LIST_DEFAULTS });
  assert.deepEqual(
    stateFromParams({ page: '3', limit: '50', sort: 'tokens', order: 'asc', q: 'pi', status: 'active', mode: 'group' }),
    { page: 3, limit: 50, sort: 'tokens', order: 'asc', q: 'pi', status: 'active', mode: 'group' },
  );
  assert.deepEqual(
    stateFromParams({ page: '0', limit: '7', sort: 'bogus', order: 'up', status: 'x', mode: 'y' }),
    { ...KEY_LIST_DEFAULTS },
  );
});

test('paramsFromState omits defaults and round-trips', () => {
  assert.deepEqual(paramsFromState(KEY_LIST_DEFAULTS), {});
  const s = { ...KEY_LIST_DEFAULTS, page: 2, q: 'pi', status: 'active' };
  assert.deepEqual(paramsFromState(s), { page: '2', q: 'pi', status: 'active' });
  assert.deepEqual(stateFromParams(paramsFromState(s)), s);
});

test('apiQuery always pages and omits empty filters', () => {
  assert.deepEqual(apiQuery(KEY_LIST_DEFAULTS), { page: 1, limit: 25, sort: 'last_active', order: 'desc' });
  assert.deepEqual(
    apiQuery({ ...KEY_LIST_DEFAULTS, q: 'pi', status: 'disabled', mode: 'custom' }),
    { page: 1, limit: 25, sort: 'last_active', order: 'desc', q: 'pi', status: 'disabled', mode: 'custom' },
  );
});

test('nextSort toggles and resets page', () => {
  const s = { ...KEY_LIST_DEFAULTS, page: 4 };
  assert.deepEqual(nextSort(s, 'name'), { ...s, sort: 'name', order: 'desc', page: 1 });
  assert.equal(nextSort(s, 'last_active').order, 'asc');
  assert.equal(nextSort({ ...s, order: 'asc' }, 'last_active').order, 'desc');
});

test('withFilter resets page', () => {
  assert.deepEqual(withFilter({ ...KEY_LIST_DEFAULTS, page: 5 }, { q: 'x' }).page, 1);
});

test('totalPages and rangeLabel', () => {
  assert.equal(totalPages(0, 25), 1);
  assert.equal(totalPages(112, 25), 5);
  assert.equal(rangeLabel(2, 25, 112, 25), 'Showing 26\u201350 of 112');
  assert.equal(rangeLabel(5, 25, 112, 12), 'Showing 101\u2013112 of 112');
  assert.equal(rangeLabel(1, 25, 0, 0), 'No keys');
});

test('pageItems with ellipses', () => {
  assert.deepEqual(pageItems(1, 5), [1, 2, 3, 4, 5]);
  assert.deepEqual(pageItems(1, 10), [1, 2, '\u2026', 10]);
  assert.deepEqual(pageItems(5, 10), [1, '\u2026', 4, 5, 6, '\u2026', 10]);
  assert.deepEqual(pageItems(10, 10), [1, '\u2026', 9, 10]);
  assert.deepEqual(pageItems(3, 10), [1, 2, 3, 4, '\u2026', 10]);
});

test('pageAfterReload steps back from an emptied page', () => {
  assert.equal(pageAfterReload(3, 0), 2);
  assert.equal(pageAfterReload(1, 0), 1);
  assert.equal(pageAfterReload(3, 4), 3);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test web/jstest/keylist.test.mjs`
Expected: FAIL — `Cannot find module '.../web/static/js/keylist.js'`.

- [ ] **Step 3: Implement** — create `web/static/js/keylist.js`:

```js
// Pure helpers for the paged API key table on Endpoints & Keys.
// No DOM access here, so the logic can be unit-tested with `node --test`.

export const KEY_LIST_DEFAULTS = Object.freeze({
  page: 1,
  limit: 25,
  sort: 'last_active',
  order: 'desc',
  q: '',
  status: 'all',
  mode: 'any',
});

export const PAGE_SIZES = [10, 25, 50, 100];
export const SORTABLE = ['name', 'status', 'created', 'last_active', 'requests', 'tokens'];
const STATUSES = ['all', 'active', 'disabled'];
const MODES = ['any', 'all', 'group', 'custom'];

const pick = (v, allowed, def) => (allowed.includes(v) ? v : def);

// Reads list state from route params (strings), falling back to defaults
// for missing or invalid values.
export function stateFromParams(params = {}) {
  const d = KEY_LIST_DEFAULTS;
  const page = parseInt(params.page, 10);
  const limit = parseInt(params.limit, 10);
  return {
    page: page >= 1 ? page : d.page,
    limit: PAGE_SIZES.includes(limit) ? limit : d.limit,
    sort: pick(params.sort, SORTABLE, d.sort),
    order: pick(params.order, ['asc', 'desc'], d.order),
    q: typeof params.q === 'string' ? params.q : d.q,
    status: pick(params.status, STATUSES, d.status),
    mode: pick(params.mode, MODES, d.mode),
  };
}

// Route params for a state: only non-default values, so URLs stay short.
export function paramsFromState(state) {
  const out = {};
  for (const [k, def] of Object.entries(KEY_LIST_DEFAULTS)) {
    if (state[k] !== def && state[k] !== '' && state[k] != null) out[k] = String(state[k]);
  }
  return out;
}

// API query for GET /api/v1/keys (always paged).
export function apiQuery(state) {
  const q = { page: state.page, limit: state.limit, sort: state.sort, order: state.order };
  if (state.q) q.q = state.q;
  if (state.status !== 'all') q.status = state.status;
  if (state.mode !== 'any') q.mode = state.mode;
  return q;
}

// Header click: a new column starts descending; the active column toggles.
export function nextSort(state, field) {
  if (state.sort !== field) return { ...state, sort: field, order: 'desc', page: 1 };
  return { ...state, order: state.order === 'desc' ? 'asc' : 'desc', page: 1 };
}

// Applies a filter/search/limit change; always returns to page 1.
export function withFilter(state, changes) {
  return { ...state, ...changes, page: 1 };
}

export const totalPages = (total, limit) => Math.max(1, Math.ceil((total || 0) / limit));

// "Showing 26–50 of 112" (en dash). Empty list -> "No keys".
export function rangeLabel(page, limit, total, shown) {
  if (!shown) return total ? `No keys on this page (${total} total)` : 'No keys';
  const from = (page - 1) * limit + 1;
  return `Showing ${from}\u2013${from + shown - 1} of ${total}`;
}

// Page buttons with ellipses: always first, last, current ±1.
// Returns numbers and the string '…'.
export function pageItems(page, pages) {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1);
  const keep = new Set([1, pages, page - 1, page, page + 1].filter((n) => n >= 1 && n <= pages));
  const sorted = [...keep].sort((a, b) => a - b);
  const out = [];
  let prev = 0;
  for (const n of sorted) {
    if (n - prev > 1) out.push('\u2026');
    out.push(n);
    prev = n;
  }
  return out;
}

// After a delete/toggle reload: an empty page beyond page 1 steps back one page.
export function pageAfterReload(page, shown) {
  return shown === 0 && page > 1 ? page - 1 : page;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test web/jstest/keylist.test.mjs`
Expected: `# pass 8`, `# fail 0`.

- [ ] **Step 5: Commit**

```bash
git add web/static/js/keylist.js web/jstest/keylist.test.mjs
git commit -m "feat(ui): key list paging/sorting state helpers"
```

---

### Task 16: Paged, sortable key table on Endpoints & Keys

**Files:**
- Modify: `web/static/js/views/endpoints.js`

**Interfaces:**
- Consumes: Task 15 exports; `GET /api/v1/keys?page=…` (Task 13); `setRoute`, `getRoute` (`web/static/js/state.js`); `debounce`, `fmtAgo`, `fmtDateTime`, `tzLabel` (`ui.js`).
- Behaviour: URL `#/endpoints?page=2&limit=25&sort=last_active&order=desc&q=pi&status=active&mode=group` restores state on refresh/back/forward. The router calls the view's `update(params)` on every hash change.

- [ ] **Step 1: Imports and state**

Replace the two import lines for `ui.js` and `state.js` with:

```js
import { h, icon, toast, fmtNum, fmtCompact, fmtAgo, fmtDateTime, tzLabel, debounce, emptyState, formDialog, confirmDialog, searchableSelect } from '../ui.js';
import { setRoute, getRoute } from '../state.js';
import {
  stateFromParams, paramsFromState, apiQuery, nextSort, withFilter,
  totalPages, rangeLabel, pageItems, pageAfterReload, PAGE_SIZES,
} from '../keylist.js';
```

Replace `  let keysList = [];` with:

```js
  let keysList = []; // all keys (legacy list) for guides and the endpoint tester
  let keyState = stateFromParams(getRoute().params); // paged key table state (mirrors URL)
  let keyPage = { keys: [], total: 0, page: 1, limit: keyState.limit };
  let keyLoadSeq = 0;
```

- [ ] **Step 2: Replace the key table**

Replace the entire `function renderKeysCard() { ... }` (from `  function renderKeysCard() {` up to, not including, `  function openKeyModal(existingKey = null) {`) with:

```js
  // ── Paged Key Table (state mirrors the URL hash) ──
  // Writes the table state to the URL; the router calls update(), which loads it.
  function setKeyState(next) {
    setRoute('endpoints', paramsFromState(next));
  }

  // Fetches the page for keyState. If the page became empty (e.g. after a
  // delete) and is not the first, steps back one page via the URL.
  async function reloadKeyPage() {
    const seq = ++keyLoadSeq;
    try {
      const res = await api.get('/keys', apiQuery(keyState));
      if (!alive || seq !== keyLoadSeq) return;
      keyPage = res || { keys: [], total: 0, page: keyState.page, limit: keyState.limit };
      const back = pageAfterReload(keyState.page, (keyPage.keys || []).length);
      if (back !== keyState.page) {
        setKeyState({ ...keyState, page: back });
        return;
      }
    } catch (e) {
      if (!alive || seq !== keyLoadSeq) return;
      keyPage = { keys: [], total: 0, page: keyState.page, limit: keyState.limit, error: e.message };
    }
    renderKeysCard();
  }

  const keySearch = h('input', {
    class: 'input',
    type: 'search',
    placeholder: 'Search key name...',
    value: keyState.q,
    style: { width: '220px' },
    oninput: debounce((e) => setKeyState(withFilter(keyState, { q: e.target.value.trim() })), 300),
  });

  function selectEl(options, value, onChange, title) {
    const el = h('select', { class: 'select', title, onchange: (e) => onChange(e.target.value) },
      options.map(([v, l]) => h('option', { value: v }, l)));
    el.value = String(value);
    return el;
  }

  function sortHeader(label, field, extra = {}) {
    const active = keyState.sort === field;
    const arrow = active ? (keyState.order === 'desc' ? ' \u2193' : ' \u2191') : '';
    return h('th', {
      ...extra,
      class: `sortable${active ? ' sorted' : ''}${extra.class ? ' ' + extra.class : ''}`,
      title: `Sort by ${label}`,
      'aria-sort': active ? (keyState.order === 'desc' ? 'descending' : 'ascending') : 'none',
      onclick: () => setKeyState(nextSort(keyState, field)),
    }, label + arrow);
  }

  function lastActiveCell(ts) {
    const ms = ts ? Date.parse(ts) : NaN;
    if (isNaN(ms)) return h('td', { class: 'muted' }, 'Never');
    return h('td', { title: `${fmtDateTime(ms)} (${tzLabel()})` }, fmtAgo(ms));
  }

  function allowedModelsCell(k) {
    const keyMode = k.model_access_mode || (
      (!k.allowed_models || k.allowed_models.length === 0 || k.allowed_models.includes('*') || k.allowed_models.includes('all'))
        ? 'all'
        : 'custom'
    );
    if (keyMode === 'all') {
      return h('td', null,
        h('span', { class: 'badge ok', style: { fontSize: '11px', display: 'inline-flex', alignItems: 'center', gap: '4px' } },
          icon('check'), 'All Models (*)'
        )
      );
    }
    if (keyMode === 'group') {
      const gids = Array.isArray(k.model_group_ids) ? k.model_group_ids : [];
      const groupBadges = gids.map(gid => {
        const grp = modelGroupsList.find(g => g.id === gid);
        const label = grp ? grp.name : gid;
        const modelPreview = grp && grp.models ? grp.models.join(', ') : '';
        return h('span', {
          class: 'badge',
          style: { background: 'var(--accent)', color: '#fff', fontSize: '11px', marginRight: '4px', cursor: 'help', display: 'inline-flex', alignItems: 'center', gap: '4px' },
          title: modelPreview ? `Models in ${label}:\n${grp.models.join('\n')}` : label
        }, icon('sparkles'), label);
      });
      if (groupBadges.length === 0) {
        groupBadges.push(h('span', { class: 'badge err', style: { fontSize: '11px' } }, 'No groups linked'));
      }
      return h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...groupBadges));
    }
    const allowedList = Array.isArray(k.allowed_models) ? k.allowed_models : [];
    const count = allowedList.length;
    const badges = allowedList.slice(0, 2).map(m =>
      h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', marginRight: '4px' } }, m)
    );
    if (count > 2) {
      badges.push(h('span', { class: 'badge muted', style: { fontSize: '11px', cursor: 'help' }, title: allowedList.join('\n') }, `+${count - 2} more`));
    }
    return h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...badges));
  }

  function keyRow(k) {
    const raw = k.raw_key || k.key;
    const statusBtn = h('button', {
      class: `badge ${k.is_active ? 'ok' : 'muted'} badge-btn`,
      type: 'button',
      onclick: async () => {
        try {
          await api.post(`/keys/${k.id}/toggle`, { active: !k.is_active });
          toast(`Key ${k.is_active ? 'deactivated' : 'activated'}`, 'ok');
          await load();
        } catch (e) {
          toast(`Failed: ${e.message}`, 'error');
        }
      }
    }, k.is_active ? 'Active' : 'Disabled');

    const created = k.created_at ? Date.parse(k.created_at) : NaN;
    return h('tr', null,
      h('td', { class: 'strong' },
        h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '8px' } }, icon('key')),
        k.name
      ),
      h('td', null, h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)),
      allowedModelsCell(k),
      h('td', { class: 'num' }, fmtNum(k.total_requests || 0)),
      h('td', { class: 'num' }, fmtCompact(k.total_tokens || 0)),
      lastActiveCell(k.last_used_at),
      h('td', null, statusBtn),
      h('td', { class: 'muted', title: isNaN(created) ? '' : `${fmtDateTime(created)} (${tzLabel()})` }, isNaN(created) ? '-' : fmtDateTime(created).slice(0, 10)),
      h('td', { style: { textAlign: 'right' } },
        h('div', { style: { display: 'inline-flex', gap: '6px' } },
          h('button', { class: 'btn btn-sm', type: 'button', title: 'Edit Key & Model Access', onclick: () => openKeyModal(k) }, icon('pencil'), 'Edit'),
          h('button', { class: 'btn btn-sm', type: 'button', title: 'Copy NineGuard API Key', onclick: () => copyText(raw, `Key "${k.name}" copied!`) }, icon('copy'), 'Copy Key'),
          h('button', { class: 'btn btn-sm btn-danger', type: 'button', title: 'Delete Key', onclick: () => confirmDeleteKey(k) }, icon('trash'))
        )
      )
    );
  }

  function renderPager() {
    const pages = totalPages(keyPage.total, keyState.limit);
    const go = (n) => setKeyState({ ...keyState, page: n });
    const btn = (label, n, disabled, current = false) => h('button', {
      class: `btn btn-sm${current ? ' btn-primary' : ''}`,
      type: 'button',
      disabled,
      'aria-current': current ? 'page' : null,
      onclick: () => go(n),
    }, label);
    return h('div', { class: 'table-foot', style: { display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' } },
      h('span', null, rangeLabel(keyState.page, keyState.limit, keyPage.total, (keyPage.keys || []).length)),
      h('span', { class: 'spacer' }),
      btn('\u2039 Prev', keyState.page - 1, keyState.page <= 1),
      ...pageItems(keyState.page, pages).map((it) => it === '\u2026'
        ? h('span', { class: 'muted' }, '\u2026')
        : btn(String(it), it, false, it === keyState.page)),
      btn('Next \u203a', keyState.page + 1, keyState.page >= pages)
    );
  }

  // Card chrome is built once so the search box keeps focus while typing;
  // renderKeysCard() only refreshes control values and the body.
  const keyStatusSel = selectEl([['all', 'All statuses'], ['active', 'Active'], ['disabled', 'Disabled']], keyState.status,
    (v) => setKeyState(withFilter(keyState, { status: v })), 'Status');
  const keyModeSel = selectEl([['any', 'Any access mode'], ['all', 'All models'], ['group', 'Model groups'], ['custom', 'Custom list']], keyState.mode,
    (v) => setKeyState(withFilter(keyState, { mode: v })), 'Access mode');
  const keyLimitSel = selectEl(PAGE_SIZES.map((n) => [String(n), `${n} / page`]), keyState.limit,
    (v) => setKeyState(withFilter(keyState, { limit: Number(v) })), 'Page size');
  const keysBody = h('div');
  keysCard.append(
    h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
      h('div', null,
        h('h2', null, 'NineGuard Client API Keys'),
        h('p', { class: 'card-sub' }, 'Generate dedicated keys for Cursor, Cline, Pi, and developers. Every token and request is tracked per key.')
      ),
      h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: () => openKeyModal() }, icon('plus'), 'Generate New Key')
    ),
    h('div', { class: 'toolbar' }, keySearch, keyStatusSel, keyModeSel, h('span', { class: 'spacer' }), keyLimitSel),
    keysBody
  );

  function renderKeysCard() {
    if (document.activeElement !== keySearch) keySearch.value = keyState.q;
    keyStatusSel.value = keyState.status;
    keyModeSel.value = keyState.mode;
    keyLimitSel.value = String(keyState.limit);

    const filtered = keyState.q || keyState.status !== 'all' || keyState.mode !== 'any';
    if (keyPage.error) {
      keysBody.replaceChildren(emptyState('alert', 'Could not load API keys', keyPage.error));
      return;
    }
    if (!keyPage.total) {
      keysBody.replaceChildren(filtered
        ? emptyState('search', 'No keys match these filters', 'Clear the search or filters to see all keys.')
        : emptyState('key', 'No NineGuard API keys generated yet', 'Click "Generate New Key" to issue your first API key for an agent.'));
      return;
    }

    const table = h('table', { class: 'table' },
      h('thead', null,
        h('tr', null,
          sortHeader('Agent / Key Name', 'name'),
          h('th', null, 'Key Token'),
          h('th', null, 'Allowed Models'),
          sortHeader('Requests', 'requests', { class: 'num' }),
          sortHeader('Tokens', 'tokens', { class: 'num' }),
          sortHeader('Last Active', 'last_active'),
          sortHeader('Status', 'status'),
          sortHeader('Created', 'created'),
          h('th', { style: { textAlign: 'right' } }, 'Actions')
        )
      ),
      h('tbody', null, ...(keyPage.keys || []).map(keyRow))
    );

    keysBody.replaceChildren(h('div', { class: 'table-wrap' }, table), renderPager());
  }
```

- [ ] **Step 3: Load and route updates**

In `load()`, replace the line `    renderKeysCard();` with `    await reloadKeyPage();` (create/edit/toggle/delete all call `load()`, so the current page reloads and steps back if it became empty).

In the object returned by `mount`, replace `    update() {},` with:

```js
    // Router calls update() on hash changes (sort, page, filters, back/forward).
    update(params) {
      const next = stateFromParams(params);
      if (JSON.stringify(next) === JSON.stringify(keyState)) return;
      keyState = next;
      reloadKeyPage();
    },
```

- [ ] **Step 4: Syntax check**

Run: `node --check web/static/js/views/endpoints.js`
Expected: exit 0.

- [ ] **Step 5: Manual check**

Run the server, create ≥ 12 keys (e.g. `for i in $(seq 1 12); do curl -s -X POST localhost:8080/api/v1/keys -H 'Content-Type: application/json' -b <session cookie> -d "{\"name\":\"bot-$i\"}"; done`, or with `NINEGUARD_AUTH_ENABLED=false` on a scratch DB). Verify:
1. `#/endpoints?limit=10&sort=name&order=asc&page=2` shows "Showing 11–N of N", page 2 highlighted, `Agent / Key Name ↑`.
2. Clicking **Last Active** sorts desc (arrow ↓), clicking again asc; never-used keys stay at the bottom both times.
3. Typing in search keeps focus, updates after ~300 ms, resets to page 1, and the URL gains `q=`.
4. Browser Back restores the previous sort/page.
5. Delete the only key on the last page → table moves to the previous page.
6. Endpoint Tester key dropdown still lists all keys.

- [ ] **Step 6: Commit**

```bash
git add web/static/js/views/endpoints.js
git commit -m "feat(ui): paged, sortable, searchable API key table with Last Active"
```

---

### Task 17: Reports "unlinked" section; traffic links by key ID

**Files:**
- Modify: `web/static/js/views/reports.js`, `web/static/js/views/dashboard.js`, `web/static/js/views/traffic.js`, `web/static/js/filters.js`

**Interfaces:**
- Consumes: `key_id` / `unlinked` JSON fields (Task 14); `key_id` traffic filter.

- [ ] **Step 1: Reports**

In `reports.js`:

1. `const cardId = k.key_name + '||' + k.key;` → `const cardId = k.key_id ? \`id:${k.key_id}\` : \`name:${k.key_name}\`;`
2. After the key badge `h('span', { class: 'badge', ... }, icon('key'), ' ', k.key_name ),` in the card header add:

```js
            k.unlinked ? h('span', { class: 'badge muted', style: { fontSize: '10.5px' }, title: 'Traffic from a deleted key, or recorded before keys were linked by ID. Grouped by the key name at the time of the request.' }, 'unlinked') : null,
```

3. Replace `    return h('div', null, legendEl, headerEl, ...cards);` **inside `renderKeysBreakdown`** (the first occurrence, before `// ── Render Group By Model ──`) with:

```js
    // Linked keys first; traffic from deleted / unlinked keys in its own section.
    const linkedCards = cards.filter((_, i) => !sortedList[i].unlinked);
    const unlinkedCards = cards.filter((_, i) => sortedList[i].unlinked);
    return h('div', null, legendEl, headerEl, ...linkedCards,
      unlinkedCards.length ? h('h3', { class: 'muted', style: { fontSize: '12px', margin: '18px 0 8px' } }, 'Unlinked / deleted keys') : null,
      ...unlinkedCards);
```

4. In the model consumers table, replace the cell content `icon('key')), c.key_name` with:

```js
              h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '6px' } }, icon('key')),
              c.key_name,
              c.unlinked ? h('span', { class: 'badge muted', style: { fontSize: '10.5px', marginLeft: '6px' } }, 'unlinked') : null
```

- [ ] **Step 2: Dashboard → Traffic by key ID** (also fixes the existing bug: the dashboard set `api_key`, which Traffic Explorer never read)

`dashboard.js`: `onclick: () => setRoute('traffic', { api_key: k.key }),` → `onclick: () => setRoute('traffic', k.key_id ? { key_id: k.key_id } : { key: k.name || k.key }),`

`filters.js` `queryParams`: after `key: p.key || '',` add `key_id: p.key_id || '',`.

`traffic.js`:
- Both "clear" patches: add `key_id: ''` next to every `key: ''` (three places: the `clearBtn` `onclick`, the empty-state "Reset filters" button, and the patch that also clears `live` and `date`).
- `clearBtn.hidden = !(p.key || p.model ...` → `clearBtn.hidden = !(p.key || p.key_id || p.model ...`
- Row action: `action('key', 'Filter key', () => patch({ key: e.api_key_name || e.api_key }))` → `action('key', 'Filter key', () => patch(e.api_key_id ? { key_id: e.api_key_id, key: '' } : { key: e.api_key_name || e.api_key, key_id: '' }))`

- [ ] **Step 3: Syntax check**

Run: `for f in web/static/js/views/reports.js web/static/js/views/dashboard.js web/static/js/views/traffic.js web/static/js/filters.js; do node --check "$f" || exit 1; done`
Expected: exit 0.

- [ ] **Step 4: Manual check**

Rename a key that has traffic → Usage Reports shows one card under the **new** name with all its history. Delete a key with traffic → its history appears under "Unlinked / deleted keys" with an `unlinked` badge. Dashboard → click a top key → Traffic Explorer shows only that key's rows (URL has `key_id=`), **Clear** removes it.

- [ ] **Step 5: Commit**

```bash
git add web/static/js/views/reports.js web/static/js/views/dashboard.js web/static/js/views/traffic.js web/static/js/filters.js
git commit -m "feat(ui): unlinked keys section; filter traffic by key ID"
```

---

### Task 18: Phase 2 verification and deploy

- [ ] **Step 1: Full suite**

Run: `go vet ./... && go test ./... && CGO_ENABLED=0 go test -tags server ./... && node --test web/jstest/keylist.test.mjs`
Expected: all pass.

- [ ] **Step 2: Migration dry run on a copy of the production DB**

```bash
scp vm:~/nineguard/data/nineguard.db /tmp/prod-copy.db
NINEGUARD_PORT=18099 NINEGUARD_AUTH_ENABLED=false NINEGUARD_DB_FILE=/tmp/prod-copy.db go run ./cmd/nineguard
```

Expected log line: `linked historical traffic to API keys rows=<n>`. Then `curl -s 'localhost:18099/api/v1/keys?page=1&sort=last_active'` returns `total` = number of keys and RFC 3339 `last_used_at` values; `curl -s 'localhost:18099/api/v1/traffic/report?period=all'` shows `key_id` on linked groups. Stop the server; delete the copy.

- [ ] **Step 3: Back up and deploy**

On the VM: `cp data/nineguard.db data/nineguard.db.pre-key-id` then `git pull && docker compose up -d --build`. Check logs for the backfill line (first start only).

- [ ] **Step 4: Push**

```bash
git push origin main
```

---

## Spec Coverage

| Spec section | Task(s) |
|---|---|
| §3.1 Timestamp normalization (`MAX(...)` fields, keys, models) | 1, 3, 4, 11 |
| §3.2 Last Active all-time, any status | 3 (by name), 14 (by key ID) |
| §3.3 `tz` on all period endpoints; `LoadLocation`; `time/tzdata`; `ResolvePeriod` table; bucket offset modifier; DST tests | 1, 2, 5, 6, 7 |
| §3.4 Reports relative Last Active + tooltip; `Never` | 7 |
| §4.1 Schema + indexes | 9 |
| §4.2 Write path, 401 unchanged | 10 |
| §4.3 Guarded backfill, ambiguous → NULL, count logged | 9 |
| §4.4 ListKeys join; reports/cross/dashboard by key ID; "Unlinked / deleted keys"; `key_id` traffic filter | 11, 14, 17 |
| §4.5 `GET /api/v1/keys` contract, legacy mode, 400s, never-used last, stable ties, SQL-side | 12, 13 |
| §4.6 Key list UI: toolbar, columns, sortable headers, Last Active cell, pager, URL state, reset to page 1, step back on empty page, tester uses legacy | 15, 16 |
| §5 Two-phase delivery | 8, 18 |
| §6 Testing (all bullets) | 1–6, 9–15; manual UI checks in 7, 16, 17 |
