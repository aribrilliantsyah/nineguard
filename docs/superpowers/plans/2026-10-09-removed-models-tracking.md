# Removed Models Tracking (Soft-Delete)

**Status:** Implemented, not committed. Steps 1 to 6 are done, see ADR 0006. Land the reports layout fix first, since Step 4 touches Reports. Not yet checked in a browser.

**Known gaps:**
- `AggregateModels` swallows per-provider fetch errors. A sync where every provider is unreachable still counts as a success, so the dashboard's stale and failing warnings do not fire for that case. They fire only when the provider list cannot be read.
- The system Logs view has no model field, so it carries no "removed" tag.
- `providers.maskKey` panics on a 1-character API key (found during Step 3, unrelated).
**Raised:** model groups keep models that upstream no longer returns.

---

## Problem

`SyncFromProviders` (`internal/models/models.go`) hard-deletes rows from `models` when a provider stops returning them. It never touches `model_groups.models`, which is a JSON list of names.

Result:
- A model group keeps names for models that no longer exist.
- Those names still count in `models_count` and still pass the key's allow check (`IsModelAllowed`).
- A key with that group sees the model in the dashboard, but a request fails upstream with a generic error.
- After a sync there is no record that a model was ever removed. Old traffic, log and report rows only store the model name as text, so they cannot say why a model has gone.

## Decision

Keep group entries stored, treat Removed Models as unavailable, and give admins a way to clean up. Do not prune groups silently, because one truncated upstream response would destroy group membership for good. Models are soft-deleted so "removed from provider" is a fact and not a guess. See `docs/adr/0006-soft-delete-removed-models.md`.

Terms (see `GLOSSARY.md`): **Removed Model**, **Disabled Model**, **Unavailable Entry**.

## Design

### Data
- `ALTER TABLE models ADD COLUMN removed_at DATETIME` (NULL = present).
- Sync sets `removed_at` when a provider returned a non-empty list that no longer contains the model. A model that comes back clears `removed_at`.
- A provider that returns an empty list or errors marks nothing. `AggregateModels` already drops failed providers, so they never enter the synced set.
- Mass-removal guard: if one sync would mark more than 50% of a provider's models, mark none and log a WARN. Apply only if the next consecutive sync agrees. Pending state is in memory only.
- Provider deactivation no longer hard-deletes its models (sync step 1 today). Models stay and routing refuses them while the provider is inactive.
- Rows are hard-deleted only when the provider is deleted or an admin deletes the model on purpose.
- `SetModelEnabled` never touches `removed_at`. Only a sync that sees the model upstream clears it.
- Models removed before this ships cannot be recovered. Tags only appear for removals from then on.

### Behaviour

| Area | Change |
|---|---|
| Model groups | Unavailable Entries are labelled "Removed from provider". `models_count` stays the total of entries, a separate `unavailable_count` is added. Per-group "Remove unavailable" button, no bulk action. Wildcards such as `openrouter/*` are never flagged. |
| Key effective list | `GetEffectiveAllowedModels` skips Removed Models. |
| `/v1/models` | No change. It is built live from upstream. |
| Requests to a Removed Model | Forwarded to upstream. If upstream answers 404 or 400, rewrite to a 404 with code `model_removed` and a clear message. Other statuses (429, 5xx) are kept. Recorded in traffic with the message. No pre-block. |
| Models page | Removed Models appear greyed with a "Removed from provider" note. Per-row delete and a bulk "Clear removed". The confirm dialog says deleting also drops the traffic tag. No auto-purge. |
| Traffic Explorer, Log Explorer | Small "removed" tag next to the model name. Shown when `m.removed_at IS NOT NULL` via the existing `LEFT JOIN models m ON t.model = m.id` in `traffic.go`. Rows with no join row get no tag. |
| Usage Reports | Same tag on model rows. Usage history is kept. |
| Dashboard | Card "Models removed from provider", hidden at 0. Shows removed count per provider, Unavailable Entries and the keys they affect, links to the filtered Models page and the groups. Shows "last synced X ago", warning when older than about 2 hours. Shows "possible mass removal, waiting for next sync" for a provider with a pending guard. |

### Sync
- Cadence stays at 30 minutes (`main.go`). No visit-triggered fetch.
- Record the last successful sync time in memory for the dashboard. Sync errors are currently discarded in the ticker, so capture them.

### Risks
- **Truncated upstream list:** guarded by the non-empty rule, the 50% guard and auto-clear on reappearance. Flags and "unavailable" never delete data.
- **Name-only history:** rows for models that were hard-deleted earlier show no tag.
- **Size:** touches db, models, keys, proxy, dashboard and several views.

## Steps (one test each)

1. Soft-delete in `models`: migration, `SyncFromProviders` sets and clears `removed_at`, deactivation no longer deletes, 50% guard with next-sync confirmation, `ListModels` returns the flag. Tests: remove a model upstream, sync, row stays with `removed_at`. Bring it back, flag clears. Empty list marks nothing. Sync that would remove over 50% marks none, a second agreeing sync applies it.
2. Groups and keys: group view reports `unavailable_count`, per-group "Remove unavailable" endpoint, effective list and `IsModelAllowed` skip Removed Models. Test: group with a Removed Model, key allow check false, wildcard entry not flagged.
3. Proxy: rewrite upstream 404 or 400 for a Removed Model to a 404 `model_removed`, recorded in traffic. Test: stub upstream 404 gives the rewritten body and a traffic row with the message. Stub 500 keeps the original status.
4. UI tags: Models page greying, per-row delete and "Clear removed", group editor labels and button, "removed" tag in Traffic, Logs and Reports. Test: handler or query test for the flag, manual check for visuals.
5. Dashboard card: last-success sync time, removed summary endpoint, stale warning, pending-guard notice. Test: summary endpoint returns counts per provider and the stale state.
6. Docs: glossary is updated, ADR 0006 is written. Update this plan's status.

## Decisions taken
- Removed and Disabled are independent states.
- Requests to a Removed Model are forwarded. Only 404 and 400 upstream errors are rewritten, to a 404 `model_removed`. This replaces the earlier "clear 4xx without forwarding" decision.
- Provider deactivation keeps models.
- Mass removal is confirmed by a second agreeing sync, in memory.
- The Models page greying and the dashboard card are in scope.
- Done after the small plugin leftovers, so those ship together with the reports layout fix.
