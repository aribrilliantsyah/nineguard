# 0005. Token Quotas and Limiting per API Key with UTC Windows

- Status: Accepted
- Date: 2026-10-07

## Context

NineGuard provides central proxying and observability for multiple AI agents, users, and tools using issued API keys (`sk-ng-...`). Administrators need a way to cap token usage (daily, weekly, monthly, or total) per client key to avoid runaway costs, without deleting or revoking credentials.

Key design tensions:

1. **Enforcement timing**:
   - *Pre-flight estimation*: Estimate incoming prompt tokens before upstream forwarding. Hard to estimate accurately across vision, tool calls, and diverse tokenizers; may reject valid requests prematurely.
   - *Token reservation*: Reserve an estimated upper bound before forwarding and settle on response. Adds stateful locks and failure-recovery complexity.
   - *Soft post-facto*: Query cumulative tokens before forwarding. If already at or above quota, block with HTTP 429. If below quota, permit the request and record actual usage on completion.

2. **Timezone anchor**:
   - *Viewer timezone*: Dashboard uses viewer timezone (e.g. WIB / `Asia/Jakarta`). But API keys belong to machines/agents that do not send a timezone header.
   - *UTC calendar windows*: Universal anchor. Fixed reset cycle at midnight UTC.

3. **Storage & tallying**:
   - *Separate counter table*: Requires synchronized atomic increments and periodic reset jobs.
   - *Direct query via indexed log*: Use SQLite composite index `idx_traffic_key_id_ts(api_key_id, timestamp)`.

## Decision

1. **Soft Post-Facto Gating**: Check consumed tokens before forwarding. If accumulated tokens >= limit, reject with HTTP 429. An admitted request runs to completion.
2. **UTC Calendar Boundaries**:
   - `daily`: `timestamp >= datetime('now', 'start of day')` (00:00 UTC)
   - `weekly`: `timestamp >= datetime('now', 'weekday 1', '-7 days', 'start of day')` (Monday 00:00 UTC)
   - `monthly`: `timestamp >= datetime('now', 'start of month')` (1st of month 00:00 UTC)
   - `total`: All-time sum
3. **Direct SQLite Aggregate**: Compute consumption via `SELECT COALESCE(SUM(total_tokens), 0) FROM traffic_logs WHERE api_key_id = ? AND timestamp >= ?`. Backed by existing index `idx_traffic_key_id_ts`.
4. **Standard 429 Response**:
   - Header: `Retry-After: <seconds_until_reset>`
   - Body:
     ```json
     {
       "error": {
         "message": "API key token quota exceeded (<used> / <limit> tokens <period>). Resets in <H>h <M>m (at <UTC_TIME>).",
         "type": "insufficient_quota",
         "code": "quota_exceeded"
       }
     }
     ```

## Consequences

- Zero token estimation discrepancies or tokenizer drift across diverse models.
- No background reset cron jobs or distributed locking required.
- Minor overshoot possible on the single request that breaches the ceiling; acceptable trade-off for simplicity and zero proxy latency overhead.
- Quota cycles are strictly distinct from reporting periods: reporting stays viewer-timezone-aware, quotas stay UTC-anchored.
