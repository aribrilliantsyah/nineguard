# Quota Reset Schedule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins choose when quotas reset (timezone, time of day, weekday, day of month), globally and per API Key, with usage kept when the schedule changes.

**Architecture:** A `ResetSchedule` resolves per key as key schedule → global schedule → UTC default and is carried in a `ResetRule` (effective schedule plus stored Transitional Windows) into `traffic.CalcQuotaWindow`. Windows stay stateless; a schedule change writes a pin `{start, until}` per Quota Period so the running window keeps its start. UI is vanilla JS.

**Tech Stack:** Go 1.27, SQLite (`modernc.org/sqlite`, **one open connection**), vanilla ES modules, `node --test`.

**Spec:** `docs/superpowers/specs/2026-10-17-quota-reset-schedule-design.md`, plus ADR 0010, ADR 0005, ADR 0007, `GLOSSARY.md`. Where this plan and the spec differ, **this plan wins** (see "Spec corrections", applied to the spec in the last task).

## How this plan is built

Unlike the model-quotas plan, no pre-verified reference code exists: this feature was specified before any code. Each task is test-first. Write the failing tests named in the task, see them fail for the right reason, implement, run the gate. Signatures and behavior come from the spec; do not change them without recording a correction below.

## Global Constraints

- Gates (AGENTS.md §2): `CGO_ENABLED=0 go test -tags server ./...`, `CGO_ENABLED=0 go build -tags server ./...`, `node --test web/jstest/*.mjs` (use the glob), `node --check` on every edited `.js`. Rebuild and restart after UI edits (embedded `embed.FS`).
- No new Go dependencies. Raw SQL. Migrations idempotent in `internal/db/db.go` `migrate()` (same `_, _ = d.Exec("ALTER TABLE ...")` style as `quota_period`).
- **SQLite has one connection.** Never run a query while another `rows` is open; inside a transaction use only `tx`, and read all rows (close them) before issuing writes on the same `tx`. `loadLocalCache` reads Model Quotas first and closes that query.
- **Zero `ResetRule` must behave byte-identically to today.** Existing quota tests keep passing with only the new argument added.
- Never recount on a schedule change: every schedule write computes pins with `PinsForChange` in the same transaction, then `loadLocalCache()`.
- Strict timezone validation for stored schedules (`time.LoadLocation`, reject `Local`); `timeutil.LoadLocation` (silent UTC fallback) is for viewer display only.
- Tests use fake keys and in-memory SQLite like `quota_*_test.go`. Timestamps are compared in UTC.
- Atomic commits per task, conventional messages; squash before merging to `main` (AGENTS.md §1.1).

## Spec corrections

Found while building, applied to the spec in Task 8:

1. §5 rule 4: Go 1.27 resolves a skipped local time with the post-transition offset (02:30 on 2026-03-08 in `America/New_York` is 06:30Z, not 07:30Z) and a repeated time to its first occurrence. The spec now states the measured result.
2. §8.6: key reads also carry `quota_reset_at` (Key Quota window end), needed by the keys table tooltip without one request per row.
3. §8.6: every key of `GET /api/v1/quotas` carries `effective_quota_reset_schedule` and `quota_reset_schedule_source`, because model-mode keys have no `key_quota` object.
4. §10.3: also removes the "(UTC ...)" period labels in the key modal and the UTC-only tooltip on the Quotas page.
5. UI: `replaceChildren(null)` renders the text "null"; call sites pass only real nodes.

---

### Task 1: Schedule type and pure window math

**Files:** modify `internal/traffic/quota.go`, `internal/traffic/quota_test.go`; create `internal/traffic/schedule.go`, `internal/traffic/schedule_test.go`, `internal/traffic/pins.go`, `internal/traffic/pins_test.go`.

**Produces:** `ResetSchedule` (JSON `timezone,time,weekday,day`) with `Validate`, `Location`, `Equal`; `WindowPin`; `ResetRule`; `CalcQuotaWindow(period, rule, now)`; unexported `scheduleWindow(period, sched, now)`; `PinsForChange(oldRule, newSchedule, now)`; `ParseClock`/`FormatClock`.

- [ ] **Step 1:** Write the failing tests and run `go test ./internal/traffic` → FAIL (undefined).
  - `schedule_test.go`: `Validate` for good values; bad timezone (`""`, `Local`, `Not/AZone`, 65 chars); bad `time` (`24:00`, `9:5`, `ab`); weekday 0 and 8; day 0 and 32. `Location` for nil and unloadable names returns UTC.
  - `quota_test.go`: existing `TestCalcWindowBoundsUTC` cases rewritten with `ResetRule{}`; then new table tests from spec §12: daily/weekly/monthly with schedule, every weekday, Day 1/15/31, February leap and non-leap with Day 29/30/31, January → previous December, `Asia/Jakarta`, `America/New_York` across spring-forward and fall-back (gap and overlap), `Pacific/Kiritimati`, `Etc/GMT+12`. Invariants over many `now` values: `start <= now < resetAt`; window at `resetAt` starts exactly at `resetAt`; never zero length. Unloadable timezone gives the zero-rule result.
  - `pins_test.go`: the four worked examples of spec §5.2 exactly (day 1→20, day 1→10 with no retroactive Oct 10, daily 00:00→14:00 at 10:00, daily 14:00→00:00 at 16:00); `oldStart == newStart` → no pin (and drops an old one); second edit during a pin keeps original `Start`, moves `Until`; just before `Until` the pin applies, at `Until` it does not; daily and monthly pinned independently.
- [ ] **Step 2:** Implement `schedule.go` (type, validation, clock helpers), change `CalcQuotaWindow` in `quota.go` (pin check, then `scheduleWindow`), implement `scheduleWindow` per spec §5 (calendar stepping with `time.Date`, per-month clamp, shared function for start and reset, guard returning the zero-rule result with one WARN), implement `pins.go`.
- [ ] **Step 3:** Update the callers of `CalcQuotaWindow` in `traffic/quota.go` and `traffic/modelquota.go` just enough to compile, passing `ResetRule{}` for now (real rules arrive in Task 3). `CGO_ENABLED=0 go test -tags server ./internal/traffic`. Expected: PASS, existing tests unchanged apart from the argument.
- [ ] **Step 4: Commit** `git add -A internal/traffic && git commit -m "feat(traffic): reset schedule, schedule-aware quota windows, transitional pins"`

---

### Task 2: Schema and key persistence

**Files:** modify `internal/db/db.go`, `internal/keys/keys.go`, `internal/keys/modelquota.go`; create `internal/db/reset_schedule_migration_test.go`, `internal/keys/reset_schedule.go`, `internal/keys/reset_schedule_test.go`.

**Produces:** columns `api_keys.quota_reset_schedule`, `api_keys.quota_window_pins` (TEXT, JSON); `KeyInfo.ResetSchedule`, `EffectiveResetSchedule`, `ResetScheduleSource`, `ResetRule` (`json:"-"`); manager-held global schedule; `KeyUpdate.ResetSchedule *ScheduleChange`, `CreateKeyOptions.ResetSchedule`; resolution and pin parsing in one helper; `UpdateKeyAll` writes schedule and pins in its transaction.

- [ ] **Step 1:** Write failing tests, run `go test ./internal/db ./internal/keys` → FAIL.
  - Migration: both columns added once and idempotent; existing rows read NULL.
  - Resolution: key schedule beats global; none falls to global; neither gives nil effective schedule; `ResetScheduleSource` is `key`/`global`/`default`.
  - `UpdateKeyAll`: absent leaves schedule and pins; `Clear` sets NULL; object stores all four; pins from `PinsForChange` written in the same transaction; a failing step rolls back key fields, schedule and pins together.
  - Cache: `loadLocalCache`, `GetKey`, `ListKeys` and the `ValidateClientKey` SQLite fallback (keys.go:416, 651, 773, 835) all return schedule, effective schedule, source and a populated `ResetRule`. Mode and period switches keep schedule and pins.
  - Bad stored JSON (schedule or pins) is ignored with a WARN, key still loads.
- [ ] **Step 2:** Add the migration. Extend the four explicit `api_keys` scans, plus `CreateKeyWithOptions`. Add the manager global-schedule field (loaded at construction and in `loadLocalCache`, from `settings` key `quota_reset_schedule`) and a single `buildResetRule(ki)` helper used by all four load paths. `UpdateKeyAll`: when `ResetSchedule` is non-nil, compute `PinsForChange(oldRule, newEffective, now)` where `newEffective` is the new key schedule or, for `Clear`, the global one; write schedule and pins with `tx` only.
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/db ./internal/keys ./internal/handler`. Expected: PASS.
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat(keys): store reset schedule and window pins per key"`

---

### Task 3: Wire rules into every window caller

**Files:** modify `internal/traffic/quota.go`, `internal/traffic/modelquota.go`, `internal/keys/modelquota.go`, `internal/keys/overview.go`, `internal/proxy/proxy.go`, `internal/handler/upstream_quota.go`, and their existing tests for the changed signatures.

**Produces:** `GetQuotaUsage(d, keyID, period, rule, now)`, `GetModelQuotaUsage(d, keyID, modelID, period, rule, now)` and their `Manager` wrappers take a `ResetRule`; `AnnotateModelQuotas`, `QuotaOverview`, proxy checks and the upstream quota handler pass `keyInfo.ResetRule`.

- [ ] **Step 1:** Write failing tests (traffic, keys, proxy): `GetQuotaUsage`/`GetModelQuotaUsage` count only rows at or after the schedule's window start (rows either side of the boundary, in UTC); with an active pin they count from the pin's `Start`; **usage immediately after a schedule change equals usage immediately before it** (the keep guarantee); `QuotaOverview` and `AnnotateModelQuotas` return `reset_at` from the schedule or pin.
- [ ] **Step 2:** Change signatures and pass `ResetRule` at: `traffic/quota.go:143`, `traffic/modelquota.go:15`, `keys/modelquota.go:281,288`, `keys/overview.go:92`, `proxy/proxy.go:219,411,824,849`, `handler/upstream_quota.go:81`. Remove the `ResetRule{}` placeholders from Task 1.
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./...`. Expected: PASS everywhere; a key with no schedule behaves exactly as before.
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat: enforce quota windows from the effective reset schedule"`

---

### Task 4: 429 message

**Files:** modify `internal/proxy/quota.go`, `internal/proxy/quota_message_test.go`, `internal/proxy/model_quota_proxy_test.go`.

**Produces:** `quotaExceededBody(modelID, consumed, limit, period, rule, resetAt, now)` with the effective schedule's timezone in the `at` clause; days in "Resets in" for 24h or more; no hardcoded `Asia/Jakarta`.

- [ ] **Step 1:** Write failing tests: schedule `Asia/Jakarta` 14:00 daily → `(at 14:00 WIB)`; weekly/monthly → `(at 14:00 WIB on 02 Nov)`; default → `(at 00:00 UTC)`; a zone with a numeric abbreviation (e.g. `Asia/Dubai` style `+04`) shows the IANA name; `total` still `Lifetime quota, does not reset.` with no `Retry-After`; 53h remaining reads `2d 5h`; during a pin `Retry-After` equals seconds to the pin's `Until`. End to end for both Key and Model mode: a key at 4.9M / 5M stays blocked after its schedule is edited and unblocks at the new next reset.
- [ ] **Step 2:** Implement; delete the `time.LoadLocation("Asia/Jakarta")` block and the `/ UTC` suffix. Keep `type`, `code`, `param`, `Retry-After` behavior.
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/proxy`. Expected: PASS.
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat(proxy): show the key's reset timezone in quota 429 messages"`

---

### Task 5: Global schedule and preview (manager)

**Files:** create `internal/keys/global_schedule.go`, `internal/keys/global_schedule_test.go`, `internal/keys/preview.go`, `internal/keys/preview_test.go`.

**Produces:** `(*Manager).GetGlobalSchedule()`, `SetGlobalSchedule(s *ResetSchedule) (counts, error)`, `ClearScheduleOverrides() (counts, error)`, `ScheduleCounts() (following, custom int)`, `PreviewReset(s *ResetSchedule, keyID string, now) (Preview, error)`.

- [ ] **Step 1:** Write failing tests:
  - `SetGlobalSchedule` writes the `settings` row and pins **only** for keys with no schedule of their own; custom keys untouched; usage unchanged; `nil` removes it and pins followers back to the default; counts include inactive keys.
  - `ClearScheduleOverrides` sets every key's own schedule to NULL with pins computed against the global one; no usage change.
  - Failure mid-way rolls back the settings row and every pin.
  - `PreviewReset` without `keyID` is pure schedule; with `keyID` applies the key's rule and lists `transitional` periods; writes nothing (row counts identical before and after).
- [ ] **Step 2:** Implement. Single transaction per operation: read all affected rows first, close them, then write using only `tx`; reload the cache and the in-memory global schedule after commit. A shared helper computes `(key, oldRule, newEffective) → pins`, reused by Task 2's `UpdateKeyAll`.
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/keys`. Expected: PASS.
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat(keys): global reset schedule, clear overrides, reset preview"`

---

### Task 6: HTTP API

**Files:** modify `internal/handler/handler.go`, `cmd/nineguard/main.go`; create `internal/handler/reset_schedule.go`, `internal/handler/reset_schedule_test.go`.

**Produces:** `quota_reset_schedule` accepted on `POST /api/v1/keys` and `PUT|POST /api/v1/keys/{id}` (absent / `null` / object); `quota_reset_schedule`, `effective_quota_reset_schedule`, `quota_reset_schedule_source` returned on `GET /api/v1/keys` (legacy and paged), `GET /api/v1/keys/{id}` and `key_quota` of `GET /api/v1/quotas`; `GET|POST /api/v1/settings/quota-reset-schedule`; `POST /api/v1/settings/quota-reset-schedule/clear-overrides`; `POST /api/v1/quotas/reset-preview`. Same auth as `GET /api/v1/keys`.

- [ ] **Step 1:** Write failing handler tests (spec §12): each validation error is 400 and names the field (`Local`, unknown, empty, too-long timezone; `24:00`; weekday 0 and 8; day 0 and 32; missing field in the object); unused fields accepted and returned; `null` clears, absent keeps; global endpoints return `following_keys`/`custom_keys`; `null` on the global endpoint removes it; `clear-overrides` empties custom schedules; preview result equals what a subsequent save produces and writes nothing; `GET /api/v1/quotas` `reset_at` reflects a pin.
- [ ] **Step 2:** Add a strict `validateResetSchedule` in the handler (uses `ResetSchedule.Validate`), wire it into the key create/update body structs near `handler.go:~1220` and `UpdateKeyAll`, add the new handlers and routes beside the other `settings/*` routes in `main.go:287-301`.
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./... && CGO_ENABLED=0 go build -tags server ./...`. Expected: PASS.
- [ ] **Step 4: Commit** `git add -A internal cmd && git commit -m "feat(api): reset schedule on keys, global schedule and preview endpoints"`

---

### Task 7: UI

**Files:** modify `web/static/js/views/endpoints.js`, `web/static/js/views/keyquota.js`, `web/static/js/views/quotas.js`, `web/static/js/api.js` if a helper is needed; create `web/static/js/resetschedule.js` (pure helpers), `web/jstest/resetschedule.test.mjs`.

**Produces:** key-modal "Reset schedule" block; Quotas page header dialog; removal of hardcoded UTC/WIB wording.

- [ ] **Step 1:** Write failing JS tests for the pure helpers in `resetschedule.js`: which fields are needed for a set of periods (daily → time; weekly adds weekday; monthly adds day; none/total → block hidden); payload builder (`Follow global` → `null`, `Custom` → full object, UTC choice → UTC object); validation mirror of spec §8.3; label text for source (`key`/`global`/`default`). Run `node --test web/jstest/*.mjs` → FAIL.
- [ ] **Step 2:** Implement the helpers, then the key-modal block (spec §10.1): Follow global / Custom, timezone from `Intl.supportedValuesOf('timeZone')` prefilled from the global schedule else `timeZone()`, "Next reset" from `POST /quotas/reset-preview` with `key_id`, transitional note, hidden unless a daily/weekly/monthly quota is in use, sent with the one Save (`quota_reset_schedule` with `quota_mode`, quotas, access).
- [ ] **Step 3:** Quotas page header "Reset schedule" button and dialog (spec §10.2): fields, "No global schedule (UTC default)", "Applies to N keys. M keys have their own schedule.", "Clear overrides (M)" with confirm, "Next reset" from preview without `key_id`, the "Usage is kept…" line.
- [ ] **Step 4:** Replace `endpoints.js:295` tooltip with the key's real next reset in its effective timezone; run `rg -n "WIB|07:00|00:00 UTC|Asia/Jakarta" internal web` and resolve or consciously keep each hit.
- [ ] **Step 5:** `node --test web/jstest/*.mjs`; `node --check` on each edited `.js`; rebuild and restart; manual check in both themes: create a key with a monthly quota (Follow global and Custom), change the global schedule and see followers' "Next reset" move while custom keys stay, edit mid-window and read the transitional note, verify the tooltip and the 429 text with a test client, clear overrides, no page errors.
- [ ] **Step 6: Commit** `git add -A web && git commit -m "feat(ui): reset schedule in key modal and global dialog on Quotas page"`

---

### Task 8: Docs, spec status, cleanup

**Files:** modify `AGENTS.md`, `docs/ARCHITECTURE.md` (Indonesian), `docs/superpowers/specs/2026-10-17-quota-reset-schedule-design.md`.

- [ ] **Step 1:** `AGENTS.md` §4: replace "Quota windows are UTC-anchored." with "Quota windows are UTC-anchored unless an Effective Reset Schedule applies (ADR 0010); schedule edits never recount usage (Transitional Window)."
- [ ] **Step 2:** `docs/ARCHITECTURE.md`: update the quota-window explanation in Indonesian.
- [ ] **Step 3:** Apply every entry from "Spec corrections" to the spec, set `Status: Implemented`.
- [ ] **Step 4:** Final gates: `CGO_ENABLED=0 go test -tags server ./...`, `CGO_ENABLED=0 go build -tags server ./...`, `node --test web/jstest/*.mjs`. `git status` shows no `data/`, `*.db`, `auth.json`, `.env*`, or build output.
- [ ] **Step 5: Commit** `git add -A && git commit -m "docs: quota reset schedule architecture and status"`; then squash the branch to one atomic commit before merging to `main` (AGENTS.md §1.1).

## Not verified before writing this plan

- `time.Date` daylight-saving gap/overlap results on the Go version in `go.mod`; Task 1 pins them in tests.
- Pin-write cost for a global change on a large key table (one transaction over all following keys).
- Mobile layout of the new modal block and dialog, streaming responses at a schedule boundary, auth-enabled mode.
