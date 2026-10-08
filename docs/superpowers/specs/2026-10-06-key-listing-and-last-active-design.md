# API Key Listing (Paging & Sorting) and Last Active Fix — Design Spec

- Date: 2026-10-06
- Status: Draft, awaiting review
- Related: `GLOSSARY.md` (Last Active, Period), `docs/adr/0003-traffic-linked-by-key-id.md`

## 1. Problems

### 1.1 Last Active shows wrong time (timezone)

- `traffic_logs.timestamp` is written with `CURRENT_TIMESTAMP` (SQLite UTC, no zone marker): `2026-10-06 09:26:40`.
- Plain column reads come back as Go `time.Time` and serialize as `2026-10-06T09:26:40Z` (correct).
- Aggregates (`MAX(timestamp)`) come back as a **raw string without zone**: `2026-10-06 09:26:40`. Verified with `modernc.org/sqlite`.
- Browser `new Date("2026-10-06 09:26:40")` parses it as **local time**. For a UTC+7 viewer the value is 7 hours early.

Affected fields:

| Field | Source |
|---|---|
| `last_active_at` per key | `internal/traffic/traffic.go` `GetUsageReports` (keys query) |
| `last_active_at` per model | `internal/traffic/traffic.go` `GetUsageReports` (models query) |
| `last_used_at` per key | `internal/keys/keys.go` `ListKeys` |
| `last_used_at` per model | `internal/models/models.go` |

### 1.2 Last Active is period-scoped

Usage Reports compute `MAX(timestamp)` inside the selected period. With "Last Month" selected, Last Active is last month's value even if the key was used minutes ago. Glossary defines Last Active as the most recent request ever.

### 1.3 Period boundaries are UTC

All period filters (`buildDateFilter`, `buildPrevDateFilter` in `internal/traffic/traffic.go`; period filter in `internal/syslog/syslog.go`; time-series bucketing with `strftime`) use SQLite `'now'` / `'start of day'` / `'start of month'` in UTC. "Today" for a UTC+7 viewer starts at 07:00 local.

### 1.4 Traffic-to-key join is broken and slow

See ADR 0003. Only the name match works; renames detach history; same names collide; `OR`/`LIKE` join cannot be indexed.

### 1.5 Key list has no paging, sorting, search, or Last Active column

`GET /api/v1/keys` returns all keys sorted by `created_at DESC`; the table on **Gateway → Endpoints & Keys** renders all rows.

## 2. Scope

### In scope

- **Phase 1 — Time correctness**
  - Normalize every timestamp in API responses to ISO 8601 UTC with `Z`.
  - Last Active = most recent request ever, any outcome.
  - Viewer-timezone period boundaries.
- **Phase 2 — Key identity and listing**
  - `traffic_logs.api_key_id` + backfill + index.
  - All per-key stats join/group on `api_key_id`.
  - Server-side paging, sorting, search, filters on `GET /api/v1/keys`.
  - Key list UI: Last Active column, sortable headers, pager, search, filters, URL state.

### Out of scope

- Paging in Usage Reports breakdown (client-side sort stays).
- Changing the stored timestamp format of existing rows.
- Key hashing (spec 2026-10-06-plugin-system-design.md §13).

## 3. Phase 1 — Time Correctness

### 3.1 Timestamp normalization

Add a helper in a shared package (e.g. `internal/db/timefmt.go`):

```go
// NormalizeSQLiteTime converts SQLite text timestamps ("2006-01-02 15:04:05",
// with or without fractional seconds or "T"/"Z") to RFC 3339 UTC ("...Z").
// Returns "" for empty or unparseable input.
func NormalizeSQLiteTime(s string) string
```

Apply to every timestamp scanned as a string, including all `MAX(...)` results above. Fields scanned into `time.Time` are already correct.

Rule for future code: timestamps returned by the API are always RFC 3339 UTC.

### 3.2 Last Active semantics

- Usage Reports per key: `last_active_at` = `MAX(timestamp)` over **all** traffic for that key (Phase 1: by existing name grouping; Phase 2: by `api_key_id`), not restricted by period. Implemented as a separate un-filtered aggregate query joined in Go by key.
- Usage Reports per model: same, by model.
- Counts all status codes (blocked 403 and upstream errors included).
- UI label stays "Last active".

### 3.3 Viewer timezone for periods

- Frontend sends `tz` (IANA name from `Intl.DateTimeFormat().resolvedOptions().timeZone`) on every request that takes a period: traffic, traffic volume, traffic stats, traffic report, traffic export, logs, logs volume, logs export.
- Server resolves `tz` with `time.LoadLocation`. Invalid or missing → UTC.
- The Docker image already includes `tzdata` (Alpine). For the pure-Go binary, import `time/tzdata` in `cmd/nineguard/main.go` so `LoadLocation` works without system zoneinfo.
- Period boundaries are computed **in Go** as UTC instants and passed as bound parameters, replacing SQLite `'now'` modifiers:

```go
type Period struct{ From, To time.Time } // UTC instants
func ResolvePeriod(period, start, end string, loc *time.Location, now time.Time) (cur, prev Period)
```

| Period | Current range (in `loc`) |
|---|---|
| `today` | local midnight today → now |
| `yesterday` | local midnight yesterday → local midnight today |
| `7d` / `14d` / `30d` | now − N×24h → now |
| `month` | local first day of month 00:00 → now |
| `last_month` | local first day of previous month → local first day of this month |
| custom `start`/`end` | local `start` 00:00 → local `end` 23:59:59.999 |
| `all` | no bound |

`prev` is the equivalent preceding window, used by existing comparison logic.

- Time-series bucket labels (`strftime('%H:00' | '%m-%d', timestamp)`) are shifted to `loc` using the zone's UTC offset at the period start: `strftime(fmt, timestamp, '+07:00')`-style modifier generated in Go. DST transitions inside a range may shift one bucket by an hour; accepted.
- `ResolvePeriod` is unit-tested with fixed `now` and several zones (`Asia/Jakarta`, `UTC`, `America/New_York` across DST).

### 3.4 Frontend display

- **Usage Reports** Last Active: relative time via existing `fmtAgo`, exact local time in `title` tooltip. `Never` when null.
- All other `new Date(ts)` usages receive `Z`-suffixed values after 3.1; no frontend parsing changes needed beyond the display change above.

## 4. Phase 2 — Key Identity and Listing

### 4.1 Schema

```sql
ALTER TABLE traffic_logs ADD COLUMN api_key_id TEXT;
CREATE INDEX IF NOT EXISTS idx_traffic_key_id ON traffic_logs(api_key_id);
CREATE INDEX IF NOT EXISTS idx_traffic_key_id_ts ON traffic_logs(api_key_id, timestamp);
```

Follows existing `ALTER TABLE ... ADD COLUMN` migration style in `internal/db/db.go`.

### 4.2 Write path

- `traffic.LogEntry` gains `APIKeyID string`.
- `internal/proxy/proxy.go` sets it from the authenticated `KeyInfo.ID` on every `Record` call (success, 403 model/firewall/plugin, 502, upstream errors).
- Requests that fail authentication (401) are not recorded with a key ID (unchanged behaviour).

### 4.3 Backfill

Run once at startup, idempotent, guarded by a `settings` row `migration.traffic_key_id = done`:

```sql
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
  ) = 1;
```

Rows with zero or multiple matches stay `NULL`. Backfill count is written to the system log.

> Depends on `api_keys.key` still holding the raw key. If key hashing (separate task) lands first, it must keep or precompute the last 4 characters before discarding the raw key.

### 4.4 Read path changes

| Location | Change |
|---|---|
| `keys.ListKeys` | Replace `OR`/`LIKE` join with `LEFT JOIN traffic_logs t ON t.api_key_id = k.id` (or pre-aggregated subquery) |
| `traffic.GetUsageReports` keys breakdown | `GROUP BY api_key_id`; name from `api_keys` (current name); `NULL` group rendered as **"Unlinked / deleted keys"**, sub-grouped by historical `api_key_name` |
| Key × model cross breakdown | Same grouping on `api_key_id` |
| Traffic filter by key | Accept `key_id` param; filter `api_key_id = ?`. Existing name/masked filters keep working |
| Dashboard top keys / active keys | Group on `api_key_id` with same NULL handling |

Rows whose `api_key_id` refers to a deleted key appear under "Unlinked / deleted keys" with their historical name.

### 4.5 `GET /api/v1/keys` contract

Without `page`: unchanged — `{"keys": [...]}` with all keys (used by Traffic Explorer filter dropdown and external scripts).

With `page`:

```
GET /api/v1/keys?page=1&limit=25&sort=last_active&order=desc&q=pi&status=active&mode=group
```

| Param | Values | Default |
|---|---|---|
| `page` | ≥ 1 | — (absent = legacy mode) |
| `limit` | 10, 25, 50, 100 (others clamped to nearest allowed) | 25 |
| `sort` | `name`, `status`, `created`, `last_active`, `requests`, `tokens` | `last_active` |
| `order` | `asc`, `desc` | `desc` |
| `q` | case-insensitive substring of key name | empty |
| `status` | `active`, `disabled`, `all` | `all` |
| `mode` | `all`, `group`, `custom`, `any` | `any` |

Response:

```json
{
  "keys": [ { ...KeyInfo, "total_requests": 28, "total_tokens": 2512544, "last_used_at": "2026-10-06T09:26:40Z" } ],
  "total": 4,
  "page": 1,
  "limit": 25
}
```

Rules:

- Never-used keys (`last_used_at` null, zero requests/tokens) always sort **last**, for both `asc` and `desc`.
- Ties broken by `created_at DESC`, then `id`, so paging is stable.
- Stats are all-time.
- `page` beyond last page returns empty `keys` with correct `total`.
- Invalid `sort`/`order`/`status`/`mode` → 400 with message listing allowed values.
- Sorting, filtering, and paging happen in SQL; no full-table load in Go.

### 4.6 Key list UI (Gateway → Endpoints & Keys)

- Toolbar above table: search box (debounced 300 ms), status filter, access-mode filter, page-size select.
- Columns: Agent / Key Name, Key Token, Allowed Models, Requests, Tokens, **Last Active**, Status, Created, Actions.
- Sortable headers: Name, Status, Created, Last Active, Requests, Tokens. Click toggles `desc` → `asc`; active column shows arrow.
- Last Active cell: `fmtAgo` with exact local time in tooltip; `Never` for unused keys.
- Pager below table: `‹ Prev`, page numbers (with ellipsis), `Next ›`, and "Showing 26–50 of 112".
- State in URL hash: `#/endpoints?page=2&limit=25&sort=last_active&order=desc&q=pi&status=active&mode=group`. Refresh and back/forward restore state. Changing search/filter/limit resets to page 1.
- After create/toggle/delete, reload current page; if it becomes empty and `page > 1`, go to previous page.
- Live Endpoint Tester key dropdown keeps using legacy (unpaged) call.

## 5. Delivery

Two separate commits/PRs, in order:

1. **Phase 1** (§3) — small, user-visible fix; deploy to VM first.
2. **Phase 2** (§4) — migration, API, UI.

## 6. Testing

### Phase 1

- `NormalizeSQLiteTime`: `"2026-10-06 09:26:40"`, with fractional seconds, with `T`, with `Z`, empty, garbage.
- `ResolvePeriod`: every period × zones `UTC`, `Asia/Jakarta`, `America/New_York` (incl. DST boundary), fixed `now`; `prev` windows.
- Usage Reports: `last_active_at` ignores period (insert rows last month and today; query "last_month" → Last Active is today).
- Last Active includes 403 rows.
- Handler: invalid `tz` falls back to UTC; `tz` propagated to traffic and logs queries.
- API JSON: every timestamp field ends with `Z`.

### Phase 2

- Migration on fresh DB and on DB with existing traffic: column + indexes exist; backfill links unique matches, leaves ambiguous/unmatched `NULL`; guard prevents re-run.
- Proxy writes `api_key_id` on success, 403, 502 paths.
- Renaming a key keeps its stats; two keys with same name have separate stats.
- `ListKeys` paging: totals, page bounds, limit clamping, each sort field asc/desc, never-used last in both orders, stable tie-break, search, status/mode filters, 400 on invalid params, legacy mode unchanged.
- Usage Reports: "Unlinked / deleted keys" group for `NULL` and deleted-key IDs.
- Traffic filter by `key_id`.
- `go test ./...` and `CGO_ENABLED=0 go test -tags server ./...`.
- Manual UI check: sort headers, pager, URL state across refresh/back, Traffic Explorer dropdown still lists all keys.
