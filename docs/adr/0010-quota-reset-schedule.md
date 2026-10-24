# 0010. Quota Reset Schedule: global default, per-key override, usage kept on change

- Status: Accepted
- Date: 2026-10-17
- Amends: [0005](0005-token-quotas-utc-windows.md) Decision #2 (UTC Calendar Boundaries)

## Context

ADR 0005 anchored every Quota Window to UTC calendar boundaries. It rejected the viewer timezone because API keys belong to machines that send no timezone header. Admins now need to choose when quotas reset: a time of day for daily, a weekday for weekly, a day of month for monthly, in a timezone that matches how they budget (e.g. `Asia/Jakarta`), not UTC. They want to set it once for every key that has a quota (Key Quota and Model Quotas alike) and still let one key differ.

The viewer's timezone (the dashboard `?tz=` param) is still unusable for enforcement, since the proxy has no browser. The timezone must be stored.

A second tension: windows are stateless (ADR 0005). The window start is recomputed from the schedule on every check. Changing the schedule would therefore change the window start and recount usage over a different span: a key at 4.9M / 5M could drop to 0.3M, and one at 0.9M could jump past its limit, the moment an admin presses Save. Admins expect the opposite: usage stays, and only the reset date moves.

Options considered:

1. **Per-key schedule only.** No global setting.
2. **Global schedule only.** One setting, no per-key columns, but every key resets at the same moment.
3. **Global default with per-key override.**
4. **Schedule per quota row** (Key Quota and each Model Quota separately).
5. **Recount on change.** Stateless; usage is summed from the new window start.
6. **Fresh start on change.** Usage goes to zero.
7. **Keep on change.** The window running at the moment of the edit keeps its start and ends at the first reset of the new schedule.

## Decision

1. A **Quota Reset Schedule** is made of a timezone (IANA name), a time of day (minute precision), a weekday (1-7) and a day of month (1-31). One schedule serves the Key Quota and all Model Quotas of a key (option 4 rejected: one reset time to explain per person).
2. **Global default with per-key override** (option 3). An API Key resolves its schedule as: its own schedule, else the global one, else the ADR 0005 default (00:00 UTC, Monday, the 1st). A key can pin itself to UTC explicitly by holding a UTC schedule; having none means "follow the global".
3. **The global schedule is unset by default.** Upgrading changes nothing: keys keep resetting at UTC midnight until an admin sets one. Setting it is an explicit admin action and moves every key that has no schedule of its own.
4. **All-or-nothing.** A schedule always has all four fields. Fields a Quota Period does not use are kept, not rejected or cleared, so switching period restores them (same rule as the inactive Quota Mode in ADR 0007). Only out-of-range values are rejected: bad `HH:MM`, weekday outside 1-7, day outside 1-31, unknown timezone name.
5. **Changing a schedule keeps usage** (option 7). When the effective schedule of a key changes (the key's own edit, the global edit for keys that follow it, or a cleared override), each Quota Period gets a **Transitional Window**: it keeps the start of the window that was running, and ends at the first reset of the new schedule that is after now. After that, windows are computed from the new schedule as usual. That one window can be shorter or longer than normal. Usage is never recounted and no key flips to blocked or unblocked because of a schedule edit. This adds a small stored value per key and Quota Period (the start and end of the Transitional Window); it is not a counter and needs no reset job. A change that does not move a period's boundaries stores nothing for that period. Editing again during a Transitional Window keeps its original start and recomputes its end from the newest schedule.
6. **Calendar stepping, not 24h arithmetic.** Resets are built from a local date and time in the schedule's zone. A time skipped or repeated by daylight saving follows Go's `time.Date` resolution; start and end of every window come from the same function, so windows never overlap or leave a gap.
7. **Short months clamp.** Day 29-31 resets on the last day of a month that lacks it.
8. **The 429 message** names the effective schedule's timezone abbreviation instead of the hardcoded WIB/UTC pair. A key whose effective schedule is the default shows UTC. `Retry-After` is unchanged.
9. APIs keep returning resets as absolute instants; the dashboard formats them in the viewer's timezone. Upstream provider quota tracking (ADR 0009) is unaffected.

## Consequences

- Reset time is set once globally and overridden per key; reporting periods stay viewer-timezone-aware and are still distinct from quota cycles.
- `null` on a key means "follow the global schedule", not "UTC". A key that must stay on UTC once a global schedule exists needs an explicit UTC schedule.
- Changing the global schedule does not move keys with their own schedule. The admin UI shows how many keys follow the global one and how many are custom, and can clear all overrides.
- A key whose timezone is later removed from the system tz database fails to load. It falls back to the UTC default rather than blocking traffic.
- Windows are no longer purely a function of the schedule and the clock: a stored Transitional Window can override one window per edit. It expires on its own.
- One Transitional Window can differ from the normal length (e.g. day 1 to day 20 on Oct 17 gives Oct 1 to Oct 20, then Oct 20 to Nov 20). Admins see only "Next reset".
- Changing a key's Quota Period (daily to monthly) is not a schedule change and is not kept; it behaves as before.
