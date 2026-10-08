# 0002. Model Group serves as both access template and plugin scope

- Status: Accepted
- Date: 2026-10-06

## Context

Model Groups already act as access templates: an API Key in group mode may only use models in its linked groups. Plugins needed a way to apply to "a family of models" (e.g. every Claude model gets Caveman).

Two readings of "bind a plugin to a Model Group" were considered:

1. Apply to requests from **API Keys linked to the group** (group as a team or class of users).
2. Apply to requests whose **model is a member of the group**, regardless of the API Key.

## Decision

Reading 2. A plugin bound to a Model Group applies to any request whose model matches the group's members, using exactly the same pattern rules as access control. API-Key-specific behaviour is expressed by binding plugins directly to API Keys.

When a model belongs to several groups with conflicting bindings, the group with the higher Group Priority wins; ties fall back to alphabetical group name with a UI warning.

## Consequences

- One concept, two independent jobs. A key in `all` access mode still gets group-scoped plugins for a matching model.
- Editing a group's member list changes both access and plugin behaviour at once. The UI must make this visible.
- Groups gain a priority field that only matters for plugin resolution.
- Scope resolution order: API Key > Model Group (by priority) > Global.
