# Client Key Self-Service Portal (`/portal`) — Design Spec

- Date: 2026-10-19
- Status: Proposed
- Related: `GLOSSARY.md`, `docs/adr/0009-upstream-quota-tracking-and-interception.md`, `docs/adr/0010-quota-reset-schedule.md`, `docs/adr/0012-estimated-cost-stored-per-request.md`, `docs/adr/0013-upstream-usage-aggregate-only.md`, `docs/superpowers/specs/2026-10-19-usage-diagnostics-cost-summary-design.md`

---

## 1. Problem

1. **Client App Slash-Command Hijacking:**
   Agent desktop applications (such as Claude Desktop) intercept leading slashes (`/`) client-side as internal application "Skills". Typing `/quota` or `/usage` triggers client-side errors (`Unknown skill: quota`) before an HTTP request ever leaves the user's computer.
2. **Natural Language Accidental Prompt Leak:**
   When users work around client-side slash hijacking by prompting `cek /quota` or `what is my quota?`, the request is sent as standard conversation history via `/v1/messages` or `/v1/chat/completions`. NineGuard does not match command prefixes inside arbitrary text, forwarding the prompt to upstream LLMs. This consumes prompt tokens and returns unhelpful generic assistant replies ("I have no access to your billing").
3. **No Visual Self-Service for Developers:**
   Key holders have no browser-accessible interface to inspect their remaining quota, reset schedule countdown, activity heatmap, or model usage without obtaining NineGuard admin credentials or asking gateway administrators.

---

## 2. Goals & Non-Goals

### Goals

- **Browser-accessible self-service portal at `/portal`**: Lightweight, responsive web interface served directly from NineGuard single binary.
- **Key-as-identity authentication**: Users authenticate using their assigned NineGuard API key (`sk-ng-...`). No admin username/password required.
- **Zero new backend endpoints**: Reuses existing key-authenticated endpoints:
  - `GET /v1/quota` (Key allowance, reset schedule, and shared upstream pools).
  - `GET /v1/usage?range=7d|30d|all` (Tokens, sessions, heatmap, costs, models, and book comparison).
  - `GET /v1/models` (Catalog of models permitted for this key).
- **Strict data isolation**: Gateway enforces that key holders only access data tied to their own key token.
- **Reused UI components**: Reuses existing, tested JavaScript helpers (`keyusage.js`, `modelquotas.js`, `costhelpers.js`, `diagnosticshelpers.js`).
- **Client-side credential persistence**: Key saved in browser `localStorage` with explicit "Forget Key / Logout" option.

### Non-Goals

- User account sign-ups or self-service key creation (keys remain managed exclusively by administrators).
- Admin management actions (cannot edit quotas, reset passwords, change settings, or access system logs).
- Cross-key comparisons or team leaderboards visible to key holders.
- Writing to database (the portal is strictly read-only for key metrics).

---

## 3. Architecture & Routing

```text
[ Browser: /portal ]
       │
       ├── Key entered / saved in localStorage
       ▼
[ NineGuard HTTP Server ]
       │
       ├── GET /portal ─────────────► Serves web/static/portal.html (Bypasses Admin Auth Cookie)
       │
       ├── API Calls with "Authorization: Bearer sk-ng-...":
       │     ├── GET /v1/quota ──────► Handled by revProxy (Scoped to keyInfo.ID)
       │     ├── GET /v1/usage ──────► Handled by revProxy (Scoped to keyInfo.ID)
       │     └── GET /v1/models ─────► Handled by revProxy (Filtered to key allowed models)
       ▼
[ Self-Service Dashboard Rendered ]
```

### 3.1 Server Route & Middleware Exemption

In `cmd/nineguard/main.go` and `internal/auth/auth.go`:
- Route: `GET /portal` serves `web.Page("portal.html")`.
- `RequireAuthMiddleware` exempts `/portal` alongside `/healthz`, `/login`, and `/setup`.
- API calls initiated by `/portal` target standard OpenAI-compatible endpoints (`/v1/*`), which are processed by `Proxy.ServeHTTP`.
- `Proxy.ServeHTTP` validates the Bearer key, retrieves `keyInfo`, and scopes all returned telemetry to `keyInfo.ID`.

---

## 4. User Interface & Experience

### 4.1 Key Entry Screen (Unauthenticated)

When `localStorage.getItem("nineguard_client_key")` is empty:
- Minimalist login card centered on screen:
  - NineGuard logo and title "Developer Portal".
  - Input field: "Enter your NineGuard API Key (`sk-ng-...`)".
  - Button: "Access Dashboard".
  - Inline validation: makes a test call to `GET /v1/quota` with the supplied key.
  - If 401 Unauthorized: display error "Invalid or disabled API key".
  - If 200 OK: persist to `localStorage` and transition to portal view.

### 4.2 Portal View (Authenticated)

#### Header Bar
- **Key Identifier**: Displays key name (e.g. `Agent Key - Alice`) and masked key token (`sk-ng-...1234`).
- **Timezone**: Displays effective schedule timezone (e.g. `Asia/Jakarta`).
- **Range Selector**: Pills for `7 Days`, `30 Days` (default), `All Time`.
- **Session Controls**: "Refresh" button and "Forget Key / Logout" button.

#### Section 1: Quota Status & Remaining Budget
- **Key Allowance Progress Bar**:
  - Displays token budget gauge (`Used / Limit tokens remaining`).
  - Tone-coded bar (`ok` / `near` / `exhausted`).
  - Reset timer: countdown until next reset (`in 2d 4h` or `at 14:00 WIB`).
  - Supports both Key Quota and Model Quota modes.
- **Upstream Shared Capacity**:
  - Displays upstream shared capacity pools relevant to the key's allowed models (5h rolling limits, weekly limits, balance).

#### Section 2: Usage KPI Cards (Overview)
- **Requests & Quality**: Total successful requests and error count with percentage.
- **Inferred Sessions**: Inferred session count (via inactivity gap threshold).
- **Total Tokens**: Total tokens consumed with input prompt vs output completion breakdown and cache hit rate %.
- **Estimated Cost**: Formatted in display currency (e.g. `Rp 2.225.900` or `$12.34`). Omitted entirely if `ShowCost(key)` is false.
- **Behavioral Insights**: Peak usage hour, favorite model, reasoning/thinking delivered vs lost, model fallback count.

#### Section 3: Activity Heatmap
- Reuses `groupDailyIntoWeeks` and `heatmapIntensity` from `keyusage.js`.
- Calendar grid displaying daily token consumption:
  - Rows: Monday through Sunday.
  - Columns: Calendar weeks across the selected range.
  - 5-step intensity shading from low to high volume.
  - Cell tooltips showing date, tokens, requests, and cost.

#### Section 4: Model Breakdown
- Reuses model distribution table:
  - Model ID.
  - Prompt tokens, Completion tokens, Total tokens.
  - Estimated Cost (if visible).
  - Visual share percentage bar.

#### Section 5: Allowed Models Catalog
- Displays models this key is permitted to call (from `GET /v1/models`).
- Shows model ID, provider prefix, and status.

#### Section 6: Literature Comparison & Upstream
- **Book Comparison**: Literature benchmark line (e.g. `You've used ~117,869× more tokens than Animal Farm.`).
- **Upstream Cascade**: Aggregate parent gateway usage line (if present & cost enabled).

---

## 5. Security & Isolation Matrix

| Surface | Admin Dashboard (`/`) | Client Portal (`/portal`) |
|---|---|---|
| **Authentication** | Username + Password (Session Cookie) | NineGuard API Key (`sk-ng-...`) |
| **API Endpoints** | `/api/v1/*` (Management) | `/v1/*` (Proxy & Key-Scoped) |
| **Access Scope** | Global (all keys, providers, logs, settings) | Strict Isolation (only data for provided key) |
| **Provider Secrets** | View/Edit Master Keys in Vault | Never Exposed |
| **Raw Request Payloads** | Full payload inspection & replay | Never Exposed |
| **Audit Logs** | Complete system & security audit trail | Never Exposed |
| **Cost Visibility** | Always visible in USD & converted | Strictly honors `ShowCost(key)` |

---

## 6. Implementation Scope

1. **Backend Routing (`cmd/nineguard/main.go`, `internal/auth/auth.go`):**
   - Register route `GET /portal`.
   - Exempt `/portal` from `RequireAuthMiddleware`.
2. **Web Assets (`web/static/portal.html`, `web/static/js/portal.js`):**
   - Create HTML shell loading standard NineGuard CSS stylesheets (`styles/main.css`, `styles/theme.css`).
   - Create modular JavaScript controller importing existing helpers:
     - `web/static/js/costhelpers.js` (`formatCost`, `costTooltip`).
     - `web/static/js/diagnosticshelpers.js` (`formatCacheHitRate`, `thinkingBadge`).
     - `web/static/js/modelquotas.js` (`barTone`, `quotaStatus`).
     - `web/static/js/views/keyusage.js` (`groupDailyIntoWeeks`, `heatmapIntensity`).
3. **Tests:**
   - Server route tests verifying `GET /portal` returns 200 without admin cookie.
   - Verify `GET /v1/quota` and `GET /v1/usage` enforce key isolation.
   - Client JS unit tests for portal state management and key storage.
