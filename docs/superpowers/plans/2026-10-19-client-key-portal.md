# Implementation Plan: Client Key Self-Service Portal (`/portal`)

- **Goal:** Implement a browser-accessible self-service developer portal at `/portal` where API key holders authenticate with their NineGuard key (`sk-ng-...`) to view their remaining token quota, reset timer, inferred sessions, activity heatmap, model distribution, estimated cost, and allowed models catalog without requiring admin credentials or triggering client-side slash command errors.
- **Spec Reference:** `docs/superpowers/specs/2026-10-19-client-key-portal-design.md`.
- **Target Branch:** `feat/client-key-portal` (branched from `feat/usage-summary`).

---

## Global Constraints & Principles

1. **Zero New Database Schema & Endpoints:** The portal is a pure frontend interface backed by existing key-authenticated endpoints:
   - `GET /v1/quota` (Key allowance, reset schedule, and shared upstream pools).
   - `GET /v1/usage?range=7d|30d|all` (Tokens, sessions, heatmap, costs, models, and book comparison).
   - `GET /v1/models` (Catalog of models permitted for this key).
2. **Strict Key-Level Isolation:** Authentication is performed via `Authorization: Bearer <key>`. The gateway proxies all requests through `revProxy.ServeHTTP`, strictly scoping returned data to `keyInfo.ID`. Key holders cannot inspect other keys, raw traffic logs, payloads, or system settings.
3. **No Admin Rights Required:** `RequireAuthMiddleware` must exempt `/portal` so visitors without admin session cookies can load the portal page.
4. **Reused UI Logic:** Directly reuse existing pure functions and CSS components:
   - `groupDailyIntoWeeks` & `heatmapIntensity` from `web/static/js/views/keyusage.js`.
   - `quotaPercent` & `barTone` from `web/static/js/quotahelpers.js` and `web/static/js/modelquotas.js`.
   - `formatCost` & `costTooltip` from `web/static/js/costhelpers.js`.
   - `formatCacheHitRate` & `thinkingBadge` from `web/static/js/diagnosticshelpers.js`.
5. **Quality Gates:**
   - `CGO_ENABLED=0 go test -tags server ./...`
   - `CGO_ENABLED=0 go build -tags server ./...`
   - `node --test web/jstest/*.mjs`
   - `node --check web/static/js/*.js web/static/js/views/*.js`

---

## Tasks

### Task 1: Server Routing and Middleware Exemption

**Files:**
- Modify: `cmd/nineguard/main.go`
- Modify: `internal/auth/auth.go`
- Create: `cmd/nineguard/portal_route_test.go`

**Produces:**
- Exemption of `/portal` in `internal/auth/auth.go` inside `RequireAuthMiddleware`.
- Route registration for `GET /portal` in `cmd/nineguard/main.go` serving `web.Page("portal.html")`.
- Root dispatcher update in `cmd/nineguard/main.go` directing `/portal` to `mux`.
- Go HTTP test in `portal_route_test.go` verifying that `GET /portal` returns HTTP 200 without admin session cookies.

- [x] **Step 1: Write failing test in `cmd/nineguard/portal_route_test.go`**
  - Create test verifying `GET /portal` returns HTTP 200 when auth is enabled and no session cookie is provided.
  - Run `go test ./cmd/nineguard` -> FAIL (route not registered or redirects to login).

- [x] **Step 2: Update `internal/auth/auth.go` and `cmd/nineguard/main.go`**
  - Add `path == "/portal"` to `RequireAuthMiddleware` exemptions in `internal/auth/auth.go`.
  - Add `path == "/portal"` to public mux paths in `rootHandler` in `cmd/nineguard/main.go`.
  - Register `mux.HandleFunc("GET /portal", ...)` serving `web.Page("portal.html")`.

- [x] **Step 3: Run route tests**
  `CGO_ENABLED=0 go test -tags server -run TestPortalRoute ./cmd/nineguard` -> PASS.

- [x] **Step 4: Commit**
  `git add cmd/nineguard/ internal/auth/`
  `git commit -m "feat(server): register /portal route with public auth exemption"`

---

### Task 2: Portal Helpers and Unit Tests

**Files:**
- Create: `web/static/js/portalhelpers.js`
- Create: `web/jstest/portalhelpers.test.mjs`

**Produces:**
- Helper module `web/static/js/portalhelpers.js`:
  - `getStoredPortalKey()`: reads key from `localStorage`.
  - `setStoredPortalKey(key)`: trims and persists key to `localStorage`.
  - `clearStoredPortalKey()`: removes key from `localStorage`.
  - `isValidKeyFormat(key)`: validates string is non-empty and has `sk-` prefix.
  - `maskKey(key)`: formats key for display (`sk-ng-...1234`).
  - `buildHeaders(key)`: returns `{ 'Authorization': 'Bearer ' + key }`.
- Test suite in `web/jstest/portalhelpers.test.mjs`:
  - Validates key format validation, masking, and header construction.

- [x] **Step 1: Write failing JS tests in `web/jstest/portalhelpers.test.mjs`**
  - Test valid vs invalid key formats.
  - Test key masking (`sk-ng-abc12345678` -> `sk-ng-...5678`).
  - Test header builder.
  Run `node --test web/jstest/*.mjs` -> FAIL.

- [x] **Step 2: Implement `web/static/js/portalhelpers.js`**
  - Implement and export portal helpers.

- [x] **Step 3: Run JS tests**
  `node --test web/jstest/*.mjs` -> PASS.

- [x] **Step 4: Commit**
  `git add web/static/js/portalhelpers.js web/jstest/portalhelpers.test.mjs`
  `git commit -m "feat(ui): client portal key management and formatting helpers"`

---

### Task 3: Client Portal Web Assets (HTML Shell & UI Component)

**Files:**
- Create: `web/static/portal.html`
- Create: `web/static/js/portal.js`

**Produces:**
- `web/static/portal.html`:
  - Standalone HTML shell matching NineGuard visual styling (linking `styles/main.css`, `favicon.png`, `fonts`).
  - Dark/light mode theme script synchronization with `localStorage`.
  - Embedded SVG symbol sprite for Lucide icons (`i-key`, `i-shield`, `i-sparkles`, `i-activity`, `i-dollar-sign`, `i-refresh`, `i-moon`, `i-sun`, `i-log-out`).
  - Root container: `<div id="portal-app"></div>`.
  - Script entry: `<script type="module" src="/js/portal.js"></script>`.
- `web/static/js/portal.js`:
  - **Unauthenticated View (Login Card)**:
    - Centered card with NineGuard logo, title "Developer Portal".
    - Input field: "NineGuard API Key (`sk-ng-...`)".
    - Button: "Access Dashboard".
    - Validates key by executing probe to `GET /v1/quota`. If 200, stores key and renders dashboard; if 401, shows error alert.
  - **Authenticated View (Dashboard)**:
    - Top bar: App logo, Key Name / Masked Key, local timezone badge, range pills (`7 Days`, `30 Days`, `All Time`), Refresh button, "Forget Key" button.
    - Section 1: Quota budget card:
      * Token progress bar, used/remaining tokens, reset countdown timer.
      * Upstream shared capacity pools (5h rolling limits, weekly limits).
    - Section 2: Usage KPI grid (Requests, Inferred Sessions, Total Tokens with cache hit %, Est. Cost, Active Days, Peak Hour, Favorite Model, Thinking status, Fallbacks).
    - Section 3: Daily Activity Heatmap (reusing `groupDailyIntoWeeks` & `heatmapIntensity` from `keyusage.js`).
    - Section 4: Models Breakdown Table (Model, In, Out, Total, Cost, Share % bar).
    - Section 5: Allowed Models Catalog (searchable list of models key can call).
    - Section 6: Book comparison & upstream gateway usage line (if present).

- [x] **Step 1: Create `web/static/portal.html`**
  - Implement standalone HTML template with theme init and SVG icon glyphs.

- [x] **Step 2: Implement `web/static/js/portal.js`**
  - Implement unauthenticated key entry view with inline validation.
  - Implement authenticated dashboard rendering reusing tested helper modules.

- [x] **Step 3: Run JS syntax check and test suite**
  `node --check web/static/js/portal.js`
  `node --check web/static/js/portalhelpers.js`
  `node --test web/jstest/*.mjs`

- [x] **Step 4: Commit**
  `git add web/static/portal.html web/static/js/portal.js`
  `git commit -m "feat(ui): client self-service portal shell and dashboard view"`

---

### Task 4: Documentation Sync and Quality Gates

**Files:**
- Modify: `docs/ARCHITECTURE.md` (Indonesian, keep Indonesian)
- Modify: `README.md`
- Modify: `README.id.md`
- Modify: `docs/superpowers/plans/2026-10-19-client-key-portal.md`

**Produces:**
- Documentation updates noting `/portal` self-service URL.
- Indonesian description in `docs/ARCHITECTURE.md` (§3.4 Portal Pengguna Mandiri).
- Passing all backend, frontend, and build verification gates.

- [x] **Step 1: Update documentation**
  - Update `docs/ARCHITECTURE.md` with portal architecture and key isolation.
  - Update `README.md` and `README.id.md` with `/portal` documentation.

- [x] **Step 2: Run all quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`
  4. `node --check web/static/js/*.js web/static/js/views/*.js`

- [x] **Step 3: Commit**
  `git add docs/ README.md README.id.md docs/superpowers/plans/2026-10-19-client-key-portal.md`
  `git commit -m "docs: architecture and release notes for client self-service portal"`
  - Update `docs/ARCHITECTURE.md` with portal architecture and key isolation.
  - Update `README.md` and `README.id.md` with `/portal` documentation.

- [ ] **Step 2: Run all quality gates**
  1. `CGO_ENABLED=0 go test -tags server ./...`
  2. `CGO_ENABLED=0 go build -tags server ./...`
  3. `node --test web/jstest/*.mjs`
  4. `node --check web/static/js/*.js web/static/js/views/*.js`

- [ ] **Step 3: Commit**
  `git add docs/ README.md README.id.md docs/superpowers/plans/2026-10-19-client-key-portal.md`
  `git commit -m "docs: architecture and release notes for client self-service portal"`
