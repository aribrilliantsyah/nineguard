# AGENTS.md — Contributor & AI Agent Guidelines

Mandatory protocols for AI agents and automated workflows contributing to NineGuard (Go gateway + vanilla-JS dashboard, SQLite).

---

## 1. Commit Policy & History

### 1.1 One Clean Commit per Feature
- Work on a feature branch (`feat/<topic>`), never directly on `main`.
- Per-task commits during a plan are fine on the branch. **Squash them into one atomic commit before merging to `main`.** No "fix typo" or checkpoint commits in permanent history.

### 1.2 Conventional Commits
```
<type>(<scope>): <imperative description>
```
- **Types:** `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`.
- **Scopes (as used in history):** `proxy`, `keys`, `models`, `providers`, `traffic`, `db`, `plugins`, `handler`, `ui`, `web`, `plans`.
- Example: `feat(keys): per-model token quotas with exclusive quota mode`

### 1.3 Secrets & Local Data
- Never commit `data/`, `*.db`, `auth.json`, `.env*`, or real keys (`sk-ng-…`, provider keys). Use fake keys in tests and examples.
- Do not commit build output (`nineguard`, `nineguard-server`, `*.exe`).

### 1.4 Documentation Sync on Feature Completion
Before a feature is finished, check and update:
- **`GLOSSARY.md`** (repo root): new or renamed domain terms. Terms only, no implementation details.
- **`docs/adr/`**: add `NNNN-<topic>.md` only for decisions that are hard to reverse, surprising without context, and a real trade-off. Mark amended ADRs in their `Status` line.
- **`docs/superpowers/specs/`**: set `Status: Implemented` when shipped.
- **`docs/ARCHITECTURE.md`** (request lifecycle) and **`docs/FUTURE_PLANNED_FEATURES.md`** when behavior or roadmap changes.
- **`README.md`** features list for user-facing capabilities.
- Version bumps live in `internal/version/version.go` and go in their own `chore: bump version` commit.

---

## 2. Quality Gates Before Committing

1. **Go tests:** `CGO_ENABLED=0 go test -tags server ./...` must pass (the Makefile `test-server` target).
2. **Build:** `CGO_ENABLED=0 go build -tags server ./...` compiles.
3. **JS tests:** `node --test web/jstest/*.mjs` passes. Use the glob; a bare directory path fails on Node 24.
4. **UI changes:** `node --check` every edited `.js`, then check in a browser. The web files are embedded in the binary (`embed.FS`), so **rebuild and restart** after each UI edit or the browser serves stale code.
5. **Bug fixes:** write the failing test first and see it fail for the right reason.

---

## 3. Specification & Planning Workflow

1. **Grill first:** settle the design with the user (decision tree, recommended answers) and record terms in `GLOSSARY.md` and ADRs as decisions land.
2. **Spec:** `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`, approved before any code.
3. **Plan:** `docs/superpowers/plans/YYYY-MM-DD-<topic>.md` using the writing-plans format (header, global constraints, tasks with exact files, test-first steps, no placeholders).
4. **No code until the spec and plan are approved.** Investigation and docs are fine; edits to application code wait for a go-ahead.
5. Where the plan and spec disagree, record the correction in the plan and apply it to the spec when the work lands.
6. Prefer verifying plan code in a scratch copy of the repo before writing it into a plan.

---

## 4. Project-Specific Rules

- **SQLite has one open connection** (`SetMaxOpenConns(1)`). Never start a query while another `rows` is open, and inside a transaction use only the `tx` handle, or the process deadlocks. Read, close, then query.
- **Migrations are idempotent** in `internal/db/db.go` `migrate()`: `CREATE … IF NOT EXISTS`, ignored `ALTER … ADD COLUMN`, guarded backfills. Existing databases must keep working.
- **Soft Post-Facto quota enforcement (ADR 0005):** block only when usage already meets the limit; admitted requests finish. Quota windows are UTC-anchored unless an Effective Reset Schedule applies (ADR 0010); a schedule edit never recounts usage (Transitional Window), so write new window math with `traffic.CalcQuotaWindow(period, key.ResetRule, now)` and never compute a window start yourself.
- **Proxy core request modification (ADR 0011):** the proxy core modifies request bodies ONLY by injecting `stream_options.include_usage = true` on `chat` streams when the setting is enabled, stripping the synthetic usage-only SSE event from client relay. All other request transformations belong strictly in Plugins.
- **Write-time Estimated Cost (ADR 0012):** `traffic_logs.cost_usd` is stored once at write time in USD (`REAL`); rows with `usage_estimated = 1`, unpriced models, 0 total tokens, or `nineguard/system` rows store `NULL`, never `0`. Display Currency conversion happens only at display/API time using the current effective rate.
- **Cascaded Upstream Usage (ADR 0013):** Upstream gateway usage is fetched isolated via `GET /v1/usage` and cached for 5 minutes; it is presented purely as aggregate shared upstream context and never mixed into individual key metrics.
- **Chat Commands:** `/usage [7d|30d|all]` serves Usage Summary; `/quota [filter]` and `/limits` serve Quota Report. Do not use `/usage` to filter provider quotas.
- **Model names:** `traffic_logs.model` is what the client sent; `model_id` is the resolved canonical id (ADR 0008). Compare model ids case-insensitively.
- **Don't change access/disabled checks as a side effect** of unrelated work; note issues as follow-ups instead.
- **Line endings:** the repo uses CRLF working files (`autocrlf=true`). Don't reformat whole files; keep diffs minimal.
- **Language:** code, ADRs, specs and plans are in English. `docs/ARCHITECTURE.md` and `docs/AGENTS_SETUP.md` are Indonesian; keep their language when editing them.
- **Auth:** new `/api/v1/*` routes are protected by the global auth middleware. Register routes in `cmd/nineguard/main.go`.
