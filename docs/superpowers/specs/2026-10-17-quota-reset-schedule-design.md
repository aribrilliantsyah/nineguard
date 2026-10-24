# Quota Reset Schedule — Design Spec

- Date: 2026-10-17
- Status: Implemented
- Related: `GLOSSARY.md` (Quota Reset Schedule, Global Reset Schedule, Key Reset Schedule, Effective Reset Schedule, Quota Window, Transitional Window), `docs/adr/0005-token-quotas-utc-windows.md` (amended), `docs/adr/0007-model-quotas-per-key.md`, `docs/adr/0010-quota-reset-schedule.md`

## 1. Problem

Every Quota Window resets at UTC calendar boundaries (ADR 0005). Admins budget in their own timezone and want to choose the reset moment: a time of day for daily quotas, a weekday for weekly, a day of month for monthly. They want to set it once for all keys with a quota (Key Quota and Model Quotas alike), override it on single keys, and change it later without usage being recounted. Decisions are recorded in ADR 0010; this spec fixes the schema, window math, API, request path, and UI.

## 2. Scope

### In scope

- A Quota Reset Schedule (timezone, time of day, weekday, day of month) set globally and per API Key.
- Resolution: key schedule, else global, else the UTC default.
- Window math that honours the schedule and daylight saving.
- Transitional Windows: changing a schedule keeps usage; the running window keeps its start and ends at the first reset of the new schedule.
- Read/write through the key API and new global-schedule endpoints, plus a preview endpoint.
- 429 message shows the effective schedule's timezone instead of the hardcoded WIB/UTC pair.
- UI: key modal block, and a global dialog on the Quotas page.

### Out of scope

- Per-quota-row schedules (ADR 0010 #1).
- Hour-of-day resets for `total` or `none` periods (they never reset).
- Upstream provider quota tracking (ADR 0009); it keeps its own reset logic.
- Reporting periods and the viewer-timezone dashboard.
- Migrating existing keys: with no schedule anywhere, behaviour is today's.
- A recount preview ("N keys would become exhausted"): not needed, because changes never recount usage (ADR 0010 #5).

## 3. Schema

In `migrate()` (`internal/db/db.go`), idempotent, same style as `quota_period`:

```sql
ALTER TABLE api_keys ADD COLUMN quota_reset_schedule TEXT;  -- JSON, NULL = follow the global schedule
ALTER TABLE api_keys ADD COLUMN quota_window_pins    TEXT;  -- JSON, NULL = no Transitional Windows
```

`quota_reset_schedule` JSON, always all four fields:

```json
{"timezone":"Asia/Jakarta","time":"14:00","weekday":1,"day":1}
```

`quota_window_pins` JSON, keyed by Quota Period:

```json
{"monthly":{"start":"2026-10-01T00:00:00Z","until":"2026-10-20T07:00:00Z"}}
```

Global schedule: one row in `settings`, key `quota_reset_schedule`, value is the same JSON. No row (or an unparseable value) means unset.

- No backfill, no new table, no new index. Model Quota rows carry nothing (ADR 0010 #1).
- JSON is used so the four schedule fields are written and cleared atomically and a later field can be added without another `ALTER`.
- `start` and `until` are RFC 3339 UTC instants.

## 4. Domain types

New in `internal/traffic` (owns window math), shared by `keys`, `proxy`, and `handler`:

```go
// ResetSchedule says when Quota Periods reset. nil means the ADR 0005 default:
// 00:00 UTC, Monday, the 1st.
type ResetSchedule struct {
    Timezone string `json:"timezone"` // IANA name
    Time     string `json:"time"`     // "HH:MM"
    Weekday  int    `json:"weekday"`  // 1 = Monday .. 7 = Sunday
    Day      int    `json:"day"`      // 1..31
}

func (s *ResetSchedule) Validate() error           // §8.3
func (s *ResetSchedule) Location() *time.Location  // UTC if s is nil or the name no longer loads
func (s *ResetSchedule) Equal(o *ResetSchedule) bool

// WindowPin is a Transitional Window for one Quota Period.
type WindowPin struct {
    Start time.Time `json:"start"`
    Until time.Time `json:"until"`
}

// ResetRule is everything window math needs for one key.
type ResetRule struct {
    Schedule *ResetSchedule         // the Effective Reset Schedule; nil = default
    Pins     map[string]WindowPin   // by Quota Period; expired entries are ignored
}
```

`keys.KeyInfo` gains:

- `ResetSchedule *traffic.ResetSchedule` — JSON `quota_reset_schedule`: the key's own schedule, `null` = follows global.
- `EffectiveResetSchedule *traffic.ResetSchedule` — JSON `effective_quota_reset_schedule`, read-only. `null` = the UTC default.
- `ResetScheduleSource string` — JSON `quota_reset_schedule_source`, read-only: `key`, `global`, or `default`.
- `ResetRule traffic.ResetRule` — `json:"-"`, built at load: effective schedule plus parsed pins. Passed to window math.

`loadLocalCache`, `GetKey`, `ListKeys`, and the SQLite fallback in `ValidateClientKey` (`keys.go:416, 651, 773, 835`) all scan `api_keys` columns explicitly, so each reads the two new columns. The manager keeps the global schedule in memory (loaded with the cache, reloaded after a global write) and resolves the effective schedule while building each `KeyInfo`.

`keys.CreateKeyOptions` and `keys.KeyUpdate` gain a `ResetSchedule` change. Update needs to tell "absent" from "set" from "clear", so it is `ResetSchedule *ScheduleChange` with `ScheduleChange{Clear bool; Value *traffic.ResetSchedule}`; a nil pointer leaves the column untouched.

## 5. Window calculation (`internal/traffic/quota.go`)

```go
// CalcQuotaWindow returns the start of the current Quota Window and the next reset instant.
// rule == the zero ResetRule keeps the ADR 0005 UTC calendar behaviour exactly.
func CalcQuotaWindow(period string, rule ResetRule, now time.Time) (start, resetAt time.Time)

// scheduleWindow is the pure calendar part (no pins).
func scheduleWindow(period string, sched *ResetSchedule, now time.Time) (start, resetAt time.Time)
```

`CalcQuotaWindow`:

1. If `rule.Pins[period]` exists and `Start <= now < Until`, return `(Start, Until)`. This is a Transitional Window.
2. Otherwise return `scheduleWindow(period, rule.Schedule, now)`. An expired pin is ignored.

`scheduleWindow` with `sched == nil` is byte-identical to today's code, so existing tests stay valid after the signature change.

With a schedule, let `loc = sched.Location()`, `local = now.In(loc)`, and `(h, m)` be the parsed `Time`:

| Period | Reset instant `S(i)` | Window start |
|---|---|---|
| `daily` | local date `D(i)` at `h:m` | the greatest `S(i) <= now` |
| `weekly` | the local date with ISO weekday `Weekday` in week `i`, at `h:m` | the greatest `S(i) <= now` |
| `monthly` | local date `min(Day, daysInMonth)` of month `i`, at `h:m` | the greatest `S(i) <= now` |
| `total` | n/a | epoch start, reset far future (unchanged) |
| other | n/a | unchanged |

`resetAt` is the `S(i+1)` after the chosen start. Both come from the **same function** `S`, so consecutive windows share a boundary exactly and never overlap or leave a gap.

Construction rules:

1. Build each `S(i)` with `time.Date(y, mo, d, h, m, 0, 0, loc)`, stepping by calendar day/week/month on the local date. Never add `24h * n` to an instant, so windows do not drift by an hour across daylight saving changes.
2. Pick the start by testing the candidate for the current local period against `now`; if it is after `now`, use the previous period's. Periods are today/yesterday (daily), this/previous week (weekly), this/previous month (monthly).
3. Monthly clamp: `d = min(Day, daysInMonth(y, mo))`, per month. Day 31 resets on Feb 28 or 29, Apr 30, and so on.
4. A local time skipped or repeated by a daylight saving change resolves through `time.Date`, which returns a valid instant in one of the two offsets. What matters is that start and reset come from the same function, so a window is always bounded by two real instants. A unit test pins the result for `America/New_York` so a future Go release cannot change it silently. Measured on Go 1.27: a skipped time resolves with the post-transition offset (02:30 on the 2026-03-08 spring-forward day is 06:30Z, so that day's window is exactly 24h and the one before it 23h), and a repeated time uses its first occurrence (01:30 on 2026-11-01 is 05:30Z, EDT). Neither leaves a gap or an overlap.
5. A result with `resetAt <= start` or `start > now` is a bug. The function guards it by returning the default-schedule result and logging once at WARN.

Usage queries use `start.UTC()` formatted with `timeutil.SQLiteLayout`, as now.

### 5.1 Callers

Every caller passes the key's `ResetRule`:

| Caller | Change |
|---|---|
| `traffic/quota.go:143` `GetQuotaUsage(d, keyID, period, now)` | takes a `ResetRule`; the `Manager` wrapper too |
| `traffic/modelquota.go:15` `GetModelQuotaUsage` | same |
| `keys/modelquota.go:281,288` `AnnotateModelQuotas` | passes `ki.ResetRule` |
| `keys/overview.go:92` `QuotaOverview` | passes `ki.ResetRule` for the Key Quota |
| `proxy/proxy.go:219, 411, 824, 849` | pass `keyInfo.ResetRule` |
| `handler/upstream_quota.go:81` | passes `keyInfo.ResetRule` |

`proxy/chat_command.go` formats resets relatively (`formatResetIn`) from values the above produce; no change.

### 5.2 Transitional Windows (ADR 0010 #5)

```go
// PinsForChange returns the pins a key needs when its Effective Reset Schedule changes
// from oldRule to newSchedule at time now. Existing pins are carried through oldRule.
func PinsForChange(oldRule ResetRule, newSchedule *ResetSchedule, now time.Time) map[string]WindowPin
```

For each period in `daily`, `weekly`, `monthly`:

1. `oldStart, _ = CalcQuotaWindow(period, oldRule, now)`. This already includes an active pin, so a second edit during a Transitional Window keeps its original start.
2. `newStart, newReset = scheduleWindow(period, newSchedule, now)`.
3. If `oldStart` equals `newStart` the boundaries did not move: no pin. The old pin entry, if any, is dropped.
4. Otherwise `pin = {Start: oldStart, Until: newReset}`.

`newReset` is the first reset of the new schedule after `now`, so `Start <= now < Until` holds at creation, and the pin expires on its own when `Until` passes.

Worked examples (monthly, `now` = Oct 17, `Asia/Jakarta` 00:00 on the new schedule; old is the UTC default):

| Change | Window right after save | Then |
|---|---|---|
| day 1 → day 20 | Oct 1 → Oct 20 (shorter, once) | Oct 20 → Nov 20 |
| day 1 → day 10 | Oct 1 → Nov 10 (longer, once; the Oct 10 reset is not fired retroactively) | Nov 10 → Dec 10 |
| daily 00:00 → 14:00, at 10:00 | today 00:00 → today 14:00 | 14:00 → 14:00 |
| daily 14:00 → 00:00, at 16:00 | today 14:00 → tomorrow 00:00 | 00:00 → 00:00 |

Pins are stored per Quota Period because a Per Model key can have daily and monthly rows at once. A pin applies to whichever quota of the key currently uses that period; it is bounded by `Until`, so a stale pin after a period switch ends on its own.

Writers (always in the same transaction as the schedule change, then a cache reload):

| Event | Keys that get `PinsForChange` |
|---|---|
| key schedule set or edited | that key |
| key schedule cleared (key now follows global) | that key |
| global schedule set, edited, or removed | every key with no schedule of its own |
| "clear all overrides" | every key with a schedule of its own |

Create-key writes no pins (no history). Changing a key's Quota Period or Quota Mode is not a schedule change and writes none.

## 6. Request path

Order and checks are unchanged (spec 2026-10-12 §6). Only the window used by the Key Quota check (step 3) and the Model Quota check (step 8) comes from the key's `ResetRule`. Any schedule write ends with `loadLocalCache`, the path `UpdateKeyAll` already uses (`keys.go:646`), so the proxy sees it at once.

## 7. 429 responses (`internal/proxy/quota.go`)

`quotaExceededBody` gains the key's `ResetRule` and renders the `at` clause in the effective schedule's timezone only. The hardcoded `Asia/Jakarta` lookup and the `/ UTC` suffix are removed.

| Case | Message tail |
|---|---|
| daily, `Asia/Jakarta` 14:00 | `Resets in 3h 12m (at 14:00 WIB).` |
| weekly or monthly, same | `Resets in 2d 5h (at 14:00 WIB on 02 Nov).` |
| default (UTC) | `Resets in 3h 12m (at 00:00 UTC).` |
| `total` | unchanged: `Lifetime quota, does not reset.` |

- The zone label is the abbreviation Go returns (`WIB`, `JST`, `EST`). When it is not alphabetic (Go returns `+03` for some zones), the label is the IANA name.
- "Resets in" gains a day component for 24h or more (`2d 5h`, not `53h 0m`), for all keys.
- During a Transitional Window the reset is the pin's `Until`; `Retry-After` is its seconds.
- `type`, `code`, and `param` are unchanged.

## 8. API

### 8.1 Representation

```json
"quota_reset_schedule": {"timezone": "Asia/Jakarta", "time": "14:00", "weekday": 1, "day": 1}
```

`null` on a key means it follows the global schedule. Responses also carry `effective_quota_reset_schedule` and `quota_reset_schedule_source` (`key` | `global` | `default`).

### 8.2 Key create/update

`POST /api/v1/keys` and `PUT|POST /api/v1/keys/{id}` accept `quota_reset_schedule`:

- **absent**: create stores none (follows global); update leaves it unchanged.
- **`null`**: update clears the key's own schedule (it follows global); create stores none.
- **object**: sets the full schedule. A missing field is a 400 (all-or-nothing), not defaulted.

The write, any pins from §5.2, and the other key fields share one transaction (`UpdateKeyAll`). A key that must stay on UTC while a global schedule exists is sent `{"timezone":"UTC","time":"00:00","weekday":1,"day":1}`.

### 8.3 Validation (400, message names the field)

1. `timezone`: non-empty, at most 64 characters, loadable by `time.LoadLocation`. `Local` and unknown names are rejected. This differs from `timeutil.LoadLocation`, which falls back to UTC for viewer display; a stored enforcement setting must surface a typo.
2. `time`: `HH:MM`, hour 00-23, minute 00-59.
3. `weekday`: integer 1-7.
4. `day`: integer 1-31.

Fields the key's Quota Period does not use are accepted and stored (ADR 0010 #4). A schedule is also accepted on keys with period `none` or `total`; it has no effect.

### 8.4 Global schedule

Same auth as `GET /api/v1/keys`.

```
GET  /api/v1/settings/quota-reset-schedule
POST /api/v1/settings/quota-reset-schedule             body: {"quota_reset_schedule": <object|null>}
POST /api/v1/settings/quota-reset-schedule/clear-overrides
```

`GET` and both `POST`s return:

```json
{
  "quota_reset_schedule": {"timezone":"Asia/Jakarta","time":"14:00","weekday":1,"day":1},
  "following_keys": 31,
  "custom_keys": 3
}
```

- `POST` with an object sets it; `null` removes it (keys that followed it return to the UTC default). Validation as §8.3. One transaction: the settings row, pins for every key that follows (§5.2), cache reload.
- `clear-overrides` sets every key's own schedule to `NULL` so all keys follow the global one, with pins for each (§5.2). It touches no usage.
- `following_keys` and `custom_keys` count all API Keys, including inactive ones, since an inactive key can be re-enabled.

### 8.5 Preview

```
POST /api/v1/quotas/reset-preview
body: {"quota_reset_schedule": <object|null>, "key_id": "<optional>"}
```

Returns the next reset per period for the proposed schedule, as the server would compute it after saving:

```json
{"daily":"2026-10-17T07:00:00Z","weekly":"2026-10-19T07:00:00Z","monthly":"2026-11-01T07:00:00Z",
 "transitional": ["monthly"]}
```

- `key_id` present: the key's current `ResetRule` and the pins from §5.2 are applied, so a key mid-window shows its real next reset; `transitional` lists the periods that would get a Transitional Window.
- `key_id` absent: pure schedule, no pins. This is the form the global dialog uses for the "Next reset" line.
- Read-only, writes nothing. Input validated as §8.3.

### 8.6 Reads

The three representation fields of §8.1 are returned on `GET /api/v1/keys` (legacy and paged), `GET /api/v1/keys/{id}`, and in the `key_quota` object of `GET /api/v1/quotas`. `reset_at` fields everywhere stay absolute ISO UTC instants (ADR 0010 #9). No other response shape changes, apart from these two additions the UI needed:

- `quota_reset_at` on every key read (list, paged list, single): the end of the Key Quota's current window as an absolute UTC instant, `null` when the key has no resetting Key Quota (`none` or `total`). The keys table tooltip uses it, so it needs no request per row.
- `effective_quota_reset_schedule` and `quota_reset_schedule_source` on every key of `GET /api/v1/quotas`, not only inside `key_quota`. A key in model mode has no `key_quota` object, and the Quotas page needs the timezone to show each Model Quota's reset.

## 9. Behaviour on change

- **Usage is never recounted by a schedule edit** (ADR 0010 #5). A key at 4.1M / 5M stays at 4.1M; only its next reset moves.
- **Switching Quota Period** keeps the schedule; the new period starts a normal window, as before.
- **Switching Quota Mode** (ADR 0007) keeps the schedule and pins; they apply to whichever quota is enforced.
- **An unloadable timezone** (stored name later missing from the tz database, or a bad row): the window falls back to the UTC default with a WARN log. Traffic is never blocked because of it. The binary embeds `time/tzdata` (`cmd/nineguard/main.go:14`), so this should be rare.
- **An unparseable pin or global-schedule value** is ignored as if absent, with a WARN log.
- **Pins are not pruned by a timer.** Expired entries are skipped on read and dropped on the next write to the column.

## 10. UI

### 10.1 Key modal — Quota section (`views/endpoints.js`, `views/keyquota.js`)

A "Reset schedule" block under the Quota Mode control applies to both modes.

- Shown when any enforced quota uses `daily`, `weekly`, or `monthly`. Hidden when the key has no quota, only `none`, or only `total`.
- Two choices:
  - **Follow global** (default for new keys): label shows what it resolves to, e.g. "Asia/Jakarta, 14:00" or "UTC default (no global schedule set)".
  - **Custom**: Timezone, Time, and the fields the periods in use need: Weekday if a `weekly` quota is present, Day of month if a `monthly` one is (hint: "Short months use their last day"). A key that must stay on UTC picks timezone `UTC`.
- Timezone: a searchable list from `Intl.supportedValuesOf('timeZone')`. Switching to Custom prefills the global schedule if there is one, otherwise the viewer timezone (`ui.js:71`, `timeZone()`), time `00:00`, Monday, day 1.
- A "Next reset" line from `POST /api/v1/quotas/reset-preview` with the key's `key_id`; updates as the admin edits. When the preview lists a `transitional` period the line adds: "Usage is kept; the current window ends at this reset."
- One Save sends the schedule with everything else (§8.2).

### 10.2 Global dialog on the Quotas page (`views/quotas.js`)

- A "Reset schedule" button in the page header opens a dialog with the same fields as Custom above, plus a "No global schedule (UTC default)" choice.
- Beneath the fields: "Applies to N keys. M keys have their own schedule." (`following_keys`, `custom_keys`.)
- "Clear overrides (M)" button, with a confirm, when `custom_keys > 0`.
- "Next reset" line from the preview endpoint without `key_id`, plus: "Usage is kept; each key's current window ends at its next reset under this schedule."
- No recount warning, because there is no recount (§9).

### 10.3 Hardcoded UTC text to remove

- The keys table tooltip in `endpoints.js` ("Daily resets at 07:00 WIB / 00:00 UTC") becomes the key's actual next reset in its effective timezone ("resets Thu, 05 Nov 2026 09:30 WIB", or "lifetime, never resets" for `total`), from `quota_reset_at` (§8.6).
- The Key Quota period labels in the key modal no longer say "(UTC midnight)", "(UTC Monday)" or "(UTC 1st)"; the help text points to the Reset schedule block instead.
- The reset tooltip on the Quotas page shows the exact reset in the key's timezone instead of always UTC.
- Any other literal "UTC" or "WIB" quota wording turned up by the repository search in §12 is replaced the same way.

### 10.4 Keys table, Quotas page table, dashboard card

No layout change. Reset times that already render from `reset_at` (viewer-local, relative) keep working because `reset_at` is still an absolute instant.

## 11. Documentation updates

- `GLOSSARY.md` and `docs/adr/0010-quota-reset-schedule.md` (done on this branch); `0005` Status (done).
- `AGENTS.md` §4: "Quota windows are UTC-anchored" becomes "…unless an Effective Reset Schedule applies (ADR 0010); schedule edits never recount usage (Transitional Window)".
- `docs/ARCHITECTURE.md` (Indonesian, keep the language): quota window explanation.
- The 2026-10-12 model-quotas spec is not rewritten; this spec supersedes its 429 message tail for both quota types.

## 12. Testing

Go tests, in-memory SQLite like the existing `quota_*_test.go`.

**traffic — window math** (table-driven, fixed `now`)

- Zero `ResetRule`: all existing `TestCalcWindowBoundsUTC` cases pass with the new argument.
- Daily: now before and after the reset time; reset at `00:00` and `23:59`.
- Weekly: every weekday as the reset weekday; now earlier and later than the reset moment within the week; Sunday/Monday boundaries.
- Monthly: Day 1, 15, 31; now before and after the day; January rolls back to the previous December; February in leap and non-leap years with Day 29/30/31; a 30-day month with Day 31.
- Timezones: `Asia/Jakarta` (no DST); `America/New_York` across spring-forward and fall-back for a reset time in the gap and in the overlap; `Pacific/Kiritimati` (UTC+14); `Etc/GMT+12`.
- Invariants over many `now` values per case: `start <= now < resetAt`; the window starting at `resetAt` begins exactly at `resetAt`; no zero-length window.
- Unloadable timezone falls back to the zero-rule result.

**traffic — Transitional Windows**

- The four worked examples of §5.2 exactly, including day 1 → day 10 not firing the passed Oct 10 reset.
- `oldStart == newStart` writes no pin, and drops an old one.
- Second edit during a pin keeps the original `Start` and moves `Until`.
- After `Until` the rule behaves as the plain schedule (`now` just before and at `Until`).
- Daily and monthly pinned independently on one key.
- Usage: rows either side of `Start` are counted/excluded correctly; usage right after a change equals usage right before it (the "keep" guarantee).

**keys**

- Migration: two columns added once (idempotent); existing keys read nil schedule and no pins.
- Resolution: key schedule beats global; no key schedule uses global; neither gives the UTC default; `quota_reset_schedule_source` correct in each case.
- Create/update: absent leaves it, `null` clears, object sets all four atomically; failure rolls back key fields and pins.
- Cache: `ValidateClientKey` (cached and fallback), `GetKey`, `ListKeys` return schedule, effective schedule, source.
- Global write: pins appear only on keys that follow; custom keys untouched; removing the global schedule pins followers back to default; `clear-overrides` pins the formerly custom keys.
- Mode and period switches keep schedule and pins.

**handler**

- Validation: bad timezone (`Local`, unknown, empty, too long), bad `HH:MM`, weekday 0 and 8, day 0 and 32, missing field — each 400 naming the field.
- Unused fields accepted and returned.
- Global endpoints: `following_keys`/`custom_keys` counts, null removes, `clear-overrides`.
- Preview: matches what a subsequent save produces; `transitional` lists the right periods; `key_id` absent applies no pins; writes nothing.
- `GET /api/v1/quotas` returns the schedule fields in `key_quota`; `reset_at` reflects a pin.

**proxy**

- Key mode and model mode: over-limit 429 names the effective timezone; default shows `UTC`; numeric-abbreviation zone shows the IANA name.
- `Retry-After` equals seconds to the scheduled reset, or to the pin's `Until`; `total` still omits it.
- Days appear in "Resets in" for 24h or more.
- End to end: a key at 4.9M / 5M stays blocked after its schedule is edited (the "keep" guarantee), and unblocks at the new next reset.

**UI** — manual, both themes: create a key with a monthly quota, Follow global and Custom; change the global schedule and see followers' "Next reset" move while custom keys stay; edit mid-window and read the transitional note; confirm the tooltip and 429 text from a test client; clear overrides.

**Repository search checklist:** `rg -n "WIB|07:00|00:00 UTC|Asia/Jakarta"` over `internal` and `web`, each hit resolved or consciously kept.

## 13. Delivery

One branch (`feat/quota-reset-schedule`), three phases, each green before the next.

1. **Window + data**: `ResetSchedule`, `ResetRule`, `CalcQuotaWindow`, `PinsForChange`, schema, `KeyInfo` plumbing and resolution, cache reads, usage-query and caller signature changes. Legacy behaviour verified by the existing tests.
2. **API + proxy**: key create/update/read with pins, validation, global and preview endpoints, `GET /quotas`, 429 message and day formatting, `AGENTS.md` line.
3. **UI**: key modal block, Quotas page dialog, removal of hardcoded UTC text, `docs/ARCHITECTURE.md`.

Set `Status: Implemented` on this spec when shipped (`AGENTS.md` §3).
