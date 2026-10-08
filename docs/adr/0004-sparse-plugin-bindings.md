# 0004. Sparse plugin bindings and overrides model

- Status: Accepted
- Date: 2026-10-07

## Context

NineGuard supports configuring plugins at three nested scopes:
1. Global ("All keys, all models")
2. Model Group ("Models in {group}")
3. API Key ("Key {key}")

In real deployments with dozens of Model Groups and hundreds of API Keys, the overwhelming majority of scopes have no specific opinion and remain in state `inherit`.

Two storage and UI representations were considered:

1. **Dense matrix**: Pre-populate and persist a `plugin_bindings` record with state `inherit` for every (plugin, scope) tuple, and render all hundreds of scopes in the configuration drawer with paginated tables.
2. **Sparse overrides**: Persist `plugin_bindings` records only for Global and for explicit non-inherit overrides (`on` or `off`). Render Global fixed at the top, followed by a sparse list of active overrides with a picker to add new ones. Resetting a scope to `inherit` removes the override row from the database.

## Decision

We chose **Sparse overrides**:
- Database: Rows in `plugin_bindings` for non-global scopes are only created when state is `on` or `off`. Setting state to `inherit` deletes the row. Scope resolution treats missing records as `inherit`.
- UI: The plugin configuration drawer displays the Global rule fixed at top, followed by only the active scope overrides. Users click `+ Add Override` to target a specific Model Group or API Key.
- Auto-save: Overrides are saved immediately on selection change or removal, eliminating redundant per-row "Save" buttons.
- Bidirectional access: Users can also view and edit plugin overrides directly within the Key modal (Endpoints & Keys) and Model Group modal (Models).

## Consequences

- Compact database footprint with zero stale `inherit` records.
- Fast UI rendering regardless of whether the system has 5 or 5,000 API Keys.
- Clear visual signal: users immediately see which groups or keys diverge from the global rule without sifting through pages of `inherit` dropdowns.
