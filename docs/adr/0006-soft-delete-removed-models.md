# 0006. Models removed by a Provider are soft-deleted, not pruned

- Status: Accepted
- Date: 2026-10-09

## Context

`SyncFromProviders` hard-deleted a Model when its Provider stopped listing it. Model Groups store names only, so they kept entries for models that no longer exist. Those entries still counted, still passed the access check, and requests failed upstream with a generic error. After a sync nothing recorded that a model had ever been removed, so old traffic could not explain why a model was gone.

Options considered:

1. Prune Model Group entries automatically on removal.
2. Leave everything as is.
3. Keep entries stored, mark the Model as removed, let admins clean up.

## Decision

Option 3. A Model whose Provider stops listing it becomes a **Removed Model** (`removed_at` set). The record stays. Sync clears the flag when the Provider lists the Model again.

- Groups are never pruned automatically. One truncated upstream response would otherwise destroy group membership for good. Admins use a per-group "Remove unavailable" action.
- A Provider that returns an empty list or errors marks nothing. If a sync would mark more than 50% of a Provider's models, it marks none and logs a warning. The removal is applied only if the next consecutive sync agrees. The pending state lives in memory.
- Requests to a Removed Model are forwarded. Only when upstream answers 404 or 400 is the error rewritten to a 404 with code `model_removed`. Other statuses are kept. NineGuard does not pre-block, because a stale flag would block a working model.
- Removed Models are skipped in a key's effective allowed list. `/v1/models` is built live from upstream, so it needs no change.
- Deactivating a Provider no longer hard-deletes its models. Hard delete remains for deleting the Provider and for an explicit admin delete.
- Removed is independent of Disabled. Re-enabling a Removed Model does not clear the flag.

## Consequences

- Schema change: `models.removed_at`. Removals before this ships are not recoverable and carry no tag.
- Traffic, Logs, Reports and the Models page show a "removed" tag from one flag via the existing join on `models.id`.
- The dashboard shows a card for removed models with a "last synced" staleness warning. Sync keeps its 30-minute cadence.
- Removed rows accumulate until an admin deletes them. There is no auto-purge. Deleting a row also drops its tag.
- Group views report `unavailable_count` separately from `models_count`.
