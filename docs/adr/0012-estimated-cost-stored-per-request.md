# 0012. Estimated Cost is computed once and stored on each request

- Status: Accepted
- Date: 2026-10-19

## Context

NineGuard will show an Estimated Cost (API-equivalent list price) per request, per key, and per period. Prices come from a public price list refreshed automatically, plus admin overrides. Prices change: vendors cut them, the list corrects mistakes, admins add overrides for models the list does not know.

Options considered:

1. **Compute at read time** from the current price table. History is always "priced today".
2. **Compute at write time** and store `cost_usd` on the traffic row. History keeps the price that applied when the request ran.
3. Store per-row prices (four rates) and compute at read time.

## Decision

Option 2.

- When a request is recorded, the proxy prices it with the price in effect and stores `cost_usd` (USD, `REAL`) on `traffic_logs`. Rows with no matching price, or with Estimated Usage, store `NULL`, never `0`.
- Costs are always stored in USD. Display Currency conversion happens at display time with the current rate; rows never store a converted amount.
- An admin **re-price** action recomputes `cost_usd` for a chosen time range (optionally one Model) from the current prices. It is the only way history changes, and it is logged.

## Consequences

- Reports and Usage Summaries are a plain `SUM(cost_usd)`, cheap on large tables.
- A vendor price cut does not silently rewrite last month's numbers.
- A wrong price stays wrong in history until an admin re-prices that range.
- Rows recorded before this feature have `NULL` cost. They can be re-priced once, but only where token counts are real.
- Display Currency totals for old periods use today's exchange rate, not the rate of that day. Acceptable for an estimate; documented in the UI tooltip.
