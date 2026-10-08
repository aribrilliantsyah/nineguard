# 0003. Traffic is linked to API Keys by ID

- Status: Accepted
- Date: 2026-10-06

## Context

`traffic_logs` stores the API key **masked** (`sk-ng-...b1bc`) and the key **name** at request time. Key statistics joined traffic to keys with:

```sql
t.api_key = k.key OR t.api_key_name = k.name OR t.api_key LIKE '%' || k.prefix || '%'
```

Only the name match ever worked (the other two compare a raw or prefix value against a masked one). Consequences:

- Renaming a key detaches its history.
- Two keys with the same name share statistics.
- The `OR`/`LIKE` join cannot use an index and scans all traffic per key, which blocks server-side sorting by usage.
- Usage Reports group by name + masked key; masks keep only the last 4 hex characters, so distinct keys can collide.

## Decision

Add `traffic_logs.api_key_id` (indexed), written by the proxy from the authenticated key. All per-key statistics join and group on `api_key_id`.

Backfill existing rows once: set `api_key_id` where exactly one key matches both `api_key_name = name` and the masked suffix. Ambiguous or unmatched rows (deleted or renamed keys) stay `NULL`.

`api_key_name` and the masked `api_key` stay in `traffic_logs` as a historical snapshot of what the key was called at request time.

## Consequences

- Rename-safe, collision-free statistics; indexed joins make sorting and paging by usage cheap.
- Old rows that could not be matched remain attributed by name only, and do not appear in per-key statistics of current keys.
- Rows for deleted keys keep a dangling `api_key_id`; reports must handle a key ID that no longer exists.
