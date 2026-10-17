# Upstream Sub-Provider Quota Grouping & Cascaded Propagation — Design Spec

- Date: 2026-10-14
- Status: Draft / Approved Design
- Related: `GLOSSARY.md`, `docs/adr/0009-upstream-quota-tracking-and-interception.md`, `docs/superpowers/specs/2026-10-13-upstream-quota-tracker-design.md`

---

## 1. Problem Statement

NineGuard instances proxy AI completions across multi-account aggregators (such as **9router**) and cascaded parent gateways (**NineGuard Cascaded**, e.g. `my-nineguard -> nineguard-kantor -> 9router-kantor`).

In aggregators like 9router, connections belong to diverse underlying upstream service providers (e.g. `antigravity`, `copilot`, `anthropic`, `google`). 9router reports each connection with its specific provider type (`conn.Provider`) and subscription plan (`conn.Plan`).

However, the current quota merging architecture exhibits critical information loss:
1. **Aggressive Monolithic Merging**: In `internal/upstream/pool.go`, `MergeAccountPools` merges *all* connections belonging to a provider into a single `MergedPoolQuota`. Connection-level `Provider` and `Plan` fields are discarded.
2. **Bucket Collision & Confusion**: Buckets from entirely different upstream services (e.g. Copilot's `5h Session`, `Weekly (7d)` alongside Antigravity's `Claude & GPT (5h)`, `Gemini (Weekly)`) are flattened into one bucket list under the top-level provider name.
3. **Double Flattening Across Cascaded Hops**: When a child NineGuard queries a parent NineGuard via `GET /v1/quota`, all pools are re-flattened under the child's provider name (e.g. `nineguard kantor`). The end user and AI agent cannot discern whether a limit originates from Copilot, Antigravity, or direct OpenAI.
4. **Broken Chat Command Filtering**: In `/v1/chat/completions`, users cannot filter by sub-provider (e.g. `/quota copilot` or `/quota antigravity`) because the sub-provider identity was stripped upstream.

---

## 2. Goals & Non-Goals

### Goals
- **Group by Sub-Provider**: Group upstream connections by their sub-provider identity (`conn.Provider` / normalized service family) during pool calculation, producing distinct `MergedPoolQuota` entries.
- **Cascaded Propagation**: Propagate sub-provider tags through cascaded NineGuard `GET /v1/quota` calls without losing upstream origin metadata.
- **Human-Readable Family Naming**: Display composite hierarchy tags in both dashboard and agent markdown reports (e.g. `9router · Antigravity`, `nineguard kantor · Copilot`).
- **Slash Command Sub-Provider Filtering**: Enable filtering by sub-provider in agent chat commands (e.g. `/quota copilot`, `/quota antigravity`, `/quota gemini`).
- **Privacy Preservation**: Continue stripping individual connection UUIDs, personal account emails, and account counts. Aggregated pools remain strictly anonymous.
- **Graceful Backward Compatibility**: Safely fall back to monolithic provider display if querying older NineGuard instances or providers lacking sub-provider telemetry.

### Non-Goals
- Exposing personal email addresses or internal connection identifiers to downstream clients.
- Modifying 9router or external upstream APIs.
- Automatic routing based on individual bucket exhaustion (handled upstream by 9router).

---

## 3. Data Structures & Wire Format

### 3.1 `internal/upstream/types.go`

Extend `MergedPoolQuota` with sub-provider metadata:

```go
// MergedPoolQuota represents client-facing aggregated quotas grouped by provider & sub-provider family.
type MergedPoolQuota struct {
    Provider      string        `json:"provider"`                 // Gateway/provider name (e.g. "9router", "nineguard kantor")
    SubProvider   string        `json:"sub_provider,omitempty"`   // Underlying service kind (e.g. "antigravity", "copilot")
    Family        string        `json:"family"`                   // Composite display family (e.g. "9router · Antigravity")
    Buckets       []QuotaBucket `json:"buckets"`
    AllowedModels []string      `json:"allowed_models,omitempty"`
    Status        string        `json:"status"`
    Warning       bool          `json:"warning"`
}
```

---

## 4. Sub-Provider Grouping Logic

### 4.1 Grouping in `MergeAccountPools`

Refactor `MergeAccountPools` to return `[]MergedPoolQuota` instead of a single merged struct:

```go
// MergeAccountPools groups connections by their sub-provider identity and aggregates
// each group into a privacy-preserving MergedPoolQuota.
func MergeAccountPools(providerName string, connections []ConnectionQuota) []MergedPoolQuota
```

#### Resolution Algorithm
1. Inspect each connection:
   - Identify sub-provider key:
     ```go
     subKey := strings.TrimSpace(conn.Provider)
     if subKey == "" {
         subKey = strings.TrimSpace(conn.Plan)
     }
     if subKey == "" {
         subKey = "default"
     }
     ```
2. Normalize display names:
   - `"antigravity"` -> `"Antigravity"`
   - `"copilot"` -> `"Copilot"`
   - `"github"` -> `"GitHub Copilot"`
   - `"anthropic"` -> `"Anthropic"`
   - `"google"` -> `"Google"`
3. If all connections in the provider belong to `"default"` or match `providerName`, set:
   - `Family = providerName`
   - `SubProvider = ""`
4. Otherwise, set composite family name:
   - `Family = fmt.Sprintf("%s · %s", providerName, displayName)`
   - `SubProvider = strings.ToLower(subKey)`
5. Aggregate buckets within each sub-provider group using existing capacity summation and earliest reset calculations.

---

## 5. Cascaded Propagation Architecture

In cascaded topologies (`my-nineguard -> nineguard-kantor -> 9router-kantor`):

```text
┌────────────────────────────────────────────────────────────────────────┐
│ 9router-kantor                                                         │
│ ├── Conn 1: user-a@work (Provider: "antigravity")                      │
│ ├── Conn 2: user-b@work (Provider: "antigravity")                      │
│ └── Conn 3: copilot-org (Provider: "copilot")                          │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ /api/usage
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ nineguard-kantor                                                       │
│ MergeAccountPools("9router DAK", ...) groups into 2 pools:             │
│ ├── Pool 1: Family = "9router DAK · Antigravity"                       │
│ └── Pool 2: Family = "9router DAK · Copilot"                           │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ GET /v1/quota (UnifiedQuotaResponse)
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ my-nineguard (Local Instance)                                          │
│ NineGuardAdapter preserves incoming SubProvider & Family:              │
│ ├── Connection 1: Provider = "antigravity", Label = "Antigravity"      │
│ └── Connection 2: Provider = "copilot", Label = "Copilot"              │
│ MergeAccountPools("nineguard kantor", ...) produces:                   │
│ ├── Pool 1: Family = "nineguard kantor · Antigravity"                  │
│ └── Pool 2: Family = "nineguard kantor · Copilot"                      │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ /quota chat command
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│ Agent Client (Cursor / Cline / IDE)                                    │
│ Displays clear sub-provider attribution in markdown table.             │
└────────────────────────────────────────────────────────────────────────┘
```

### 5.1 `NineGuardAdapter` Changes (`internal/upstream/nineguard.go`)

When converting upstream `UnifiedQuotaResponse.UpstreamPools` into local `ConnectionQuota` objects:
- Set `ConnectionQuota.Provider = pool.SubProvider` (or extracted sub-provider from `pool.Family`).
- Set `ConnectionQuota.Plan = pool.Family`.
- This ensures downstream NineGuard instances can regroup pools accurately.

---

## 6. Chat Command Interception (`/quota`)

### 6.1 Markdown Table Formatting

In `internal/proxy/chat_command.go`, render the composite `pool.Family` in the **Provider / Family** column:

```markdown
### 🌐 Upstream Provider Capacity (Shared Pool)

| Provider / Family | Window | Used / Total | Remaining | Next Reset |
|---|---|---|---|---|
| 9router · Antigravity | Claude & GPT (5h) | 0 / 2,000 | 100% | in 4h 56m |
| 9router · Antigravity | Gemini (5h) | 363 / 2,000 | 82% | in 1h 17m |
| nineguard kantor · Antigravity | Gemini 3.1 Flash Image | 1,636 / 2,000 | ⚠️ 18% | in 2h 25m |
| nineguard kantor · Antigravity | Claude & GPT (5h) | 451 / 2,000 | 77% | in 1h 20m |
| nineguard kantor · Copilot | 5h Session | 6 / 100 | 94% | in 5d 16h |
| nineguard kantor · Copilot | Session (5h) | 7 / 100 | 93% | in 4h 14m |
| nineguard kantor · Copilot | Weekly (7d) | 51 / 100 | 49% | in 19h 44m |
| nineguard kantor · Copilot | Weekly Fable (7d) | 0 / 100 | 100% | in 19h 44m |
```

### 6.2 Argument Filter Matching

Update `filterStr` matching to check:
1. `strings.Contains(strings.ToLower(pool.Provider), filterStr)`
2. `strings.Contains(strings.ToLower(pool.SubProvider), filterStr)`
3. `strings.Contains(strings.ToLower(pool.Family), filterStr)`
4. `strings.Contains(strings.ToLower(bucket.Name), filterStr)`

Allows:
- `/quota copilot` -> filters strictly to Copilot buckets.
- `/quota antigravity` -> filters strictly to Antigravity buckets.
- `/quota kantor` -> filters strictly to office cascaded pools.

---

## 7. Web UI Dashboard

In `web/static/js/views/upstream_quotas.js`:
1. **Sub-Provider Card Separation**: Each sub-provider pool is displayed as a distinct connection block under the cascaded provider card.
2. **Badge Cleanup**: Remove redundant duplicate `Shared Upstream Pool` badges (keep single clear badge and sub-provider badge).

---

## 8. Deployment & Version Compatibility

| Scenario | Behavior |
|---|---|
| **Both Gateways Updated** | Full sub-provider visibility and cascaded tagging active end-to-end. |
| **Only Upstream Gateway Updated** | Upstream sends split pools; older downstream gateway merges them into local provider name without crashing. |
| **Only Downstream Gateway Updated** | Downstream handles legacy single-pool responses gracefully using top-level provider name. |
