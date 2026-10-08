# Graph Report - nineguard  (2026-10-07)

## Corpus Check
- 134 files · ~140,880 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 12 file(s) not represented in the graph (top: (none) 8, .woff2 2, .example 1)

## Summary
- 1214 nodes · 4071 edges · 55 communities (47 shown, 8 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 137 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Core Server & Runtime Config
- HTTP API & Auth Handlers
- Database & Query Logging
- Web Dashboard Query Filters
- Server Main & CLI Entrypoint
- Slog Logger & Concurrency Control
- Web API Key Listing View
- Dashboard UI Helpers & Routing
- Web App Shell & Navigation
- Web Dialogs & Modals
- System Telemetry & Testing Specs
- Terminal Input & Browser Launcher
- Plugin & Headroom Unit Tests
- API Key Security & Hashing
- Plugin System Implementation Plan
- Last Active Time Fix Specs
- Key Listing Implementation Plan
- User Authentication & Sessions
- Web Plugin Helpers & Tests
- Web Frontend API Client
- Database Schema Migrations & Seeding
- Web Dashboard Charting Components
- UI Popover & Select Dropdowns
- Plugin Domain Types & Metadata
- Auth Login & Reset Pages
- Built-in Caveman Style Plugin
- HTTP Plugin Protocol & Pipeline
- Plugin Manager CRUD & Lifecycle
- Model Group Access Rules
- User Profile Dashboard View
- Platform Build & Deployment Guide
- System Overview & Feature Docs
- Plugin Execution Engine Tests
- API Key Listing Paging & Limits
- Multi-Target Build Scripts
- IDE & Agent Integration Guide
- High-Level System Architecture
- Model Group Pattern Matching
- Environment Config Loader
- Plugin Conflict & Overlap Warnings
- Upstream Provider Routing Guide
- ADR-0001 HTTP Plugins Architecture
- ADR-0002 Model Group Dual Role
- ADR-0003 Key ID Traffic Linking
- ADR-0004 Sparse Plugin Bindings
- System Glossary & Terminology
- Quick Setup Guide For Agents
- Author & Contributor Metadata
- Root Package Namespace

## God Nodes (most connected - your core abstractions)
1. `h()` - 144 edges
2. `icon()` - 91 edges
3. `jsonResponse()` - 64 edges
4. `Handler` - 62 edges
5. `jsonError()` - 62 edges
6. `toast()` - 57 edges
7. `mount()` - 47 edges
8. `mount()` - 46 edges
9. `emptyState()` - 44 edges
10. `mount()` - 43 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `UserFromContext()`  [EXTRACTED]
  cmd/nineguard/main.go → internal/auth/auth.go
- `main()` --calls--> `LoadFromEnv()`  [EXTRACTED]
  cmd/nineguard/main.go → internal/config/config.go
- `main()` --calls--> `NewManager()`  [EXTRACTED]
  cmd/nineguard/main.go → internal/plugins/manager.go
- `main()` --calls--> `NewSlogHandler()`  [EXTRACTED]
  cmd/nineguard/main.go → internal/syslog/syslog.go
- `main()` --calls--> `OpenBrowser()`  [EXTRACTED]
  cmd/nineguard/main.go → internal/ui/browser.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Token Compression Plugins** — internal_plugins_builtin_caveman, internal_plugins_builtin_ponytail, internal_plugins_builtin_headroom [EXTRACTED 0.95]
- **Plugin Binding Precedence Hierarchy** — plugins_scopetype, internal_plugins_resolve_resolvepluginsforrequest [INFERRED 0.85]

## Communities (55 total, 8 thin omitted)

### Community 0 - "Core Server & Runtime Config"
Cohesion: 0.09
Nodes (20): contextKey, Config: docker-compose.yml, Config: docker.yml, Run(), HasDesktopEnvironment(), ModeName(), Run(), TestTrayHasDesktopEnvironment() (+12 more)

### Community 1 - "HTTP API & Auth Handlers"
Cohesion: 0.13
Nodes (10): UserFromContext(), getClientIP(), Handler, jsonError(), jsonResponse(), parseFilterParams(), parseSyslogFilterParams(), requestLocation() (+2 more)

### Community 2 - "Database & Query Logging"
Cohesion: 0.06
Nodes (52): FilterParams, Manager, NormalizeLevel(), parseTimeParam(), NormalizeSQLiteTime(), NullTimeString(), parseDay(), ResolvePeriod() (+44 more)

### Community 3 - "Web Dashboard Query Filters"
Cohesion: 0.08
Nodes (65): escRe(), highlight(), iso(), queryParams(), RANGE_MS, rangeControls(), RANGES, searchTerms() (+57 more)

### Community 4 - "Server Main & CLI Entrypoint"
Cohesion: 0.10
Nodes (45): main(), printHelp(), setupTestServer(), TestUnauthenticatedDashboardAPIAccessBlocked(), NewManager(), TestAuthRecovery(), TestBcryptCostWorkFactor(), TestRequireAuthMiddleware() (+37 more)

### Community 5 - "Slog Logger & Concurrency Control"
Cohesion: 0.08
Nodes (10): Manager, Provider, maskKey(), countDefaults(), NewLogHub(), NewLogHubHandler(), SlogHandler, LogEntry (+2 more)

### Community 6 - "Web API Key Listing View"
Cohesion: 0.12
Nodes (40): apiQuery(), KEY_LIST_DEFAULTS, MODES, nextSort(), PAGE_SIZES, pageAfterReload(), pageItems(), paramsFromState() (+32 more)

### Community 7 - "Dashboard UI Helpers & Routing"
Cohesion: 0.17
Nodes (40): setRoute(), emptyState(), fmtCompact(), fmtDateTime(), fmtNum(), h(), monotoneCubicBezier(), mount() (+32 more)

### Community 8 - "Web App Shell & Navigation"
Cohesion: 0.08
Nodes (37): collapsed, isAdmin(), NAV, pages(), paletteItems(), refreshAll(), renderAvatar(), renderRoute() (+29 more)

### Community 9 - "Web Dialogs & Modals"
Cohesion: 0.15
Nodes (35): api, confirmDialog(), formDialog(), menu(), passwordField(), toast(), mount(), addModel() (+27 more)

### Community 10 - "System Telemetry & Testing Specs"
Cohesion: 0.05
Nodes (38): 10. Telemetry, 11. Security, 12. Deployment Notes, 13. Related Issues (separate tasks), 14. Open Questions, 15. Testing, 16. Licensing, 1. Goal (+30 more)

### Community 11 - "Terminal Input & Browser Launcher"
Cohesion: 0.10
Nodes (23): OpenBrowser(), DecodeKeys(), EnterRawMode(), GetKeyReader(), TestDecodeKeys(), ViewLogs(), ShowMenu(), HandleTraySelection() (+15 more)

### Community 12 - "Plugin & Headroom Unit Tests"
Cohesion: 0.10
Nodes (28): TestPluginMigrationsAndSeeding(), TestTrafficKeyIDBackfill(), TestModelAllowedLogic(), ApplyHeadroom(), TestApplyHeadroom_CompressionSkipped(), TestApplyHeadroom_IncrementalAndPreservation(), TestEmbeddedPrompts(), TestGuidancePresence() (+20 more)

### Community 13 - "API Key Security & Hashing"
Cohesion: 0.12
Nodes (8): generateSecureToken(), KeyInfo, Manager, MaskKey(), matchModelPattern(), ParseAllowedModels(), SerializeAllowedModels(), TestSerializationAndParsing()

### Community 14 - "Plugin System Implementation Plan"
Cohesion: 0.07
Nodes (28): File Map, Global Constraints, Phase 1 — Schema, Types, Seed Data & Model/Provider Extensions, Phase 2 — Plugin Engine, Transformers, Resolution & Pipeline, Phase 3 — Proxy Pipeline Integration & Traffic Telemetry, Phase 4 — REST API Endpoints & Route Wiring, Phase 5 — Frontend Dashboard & Management UI, Phase 6 — Full Verification & Acceptance Checks (+20 more)

### Community 15 - "Last Active Time Fix Specs"
Cohesion: 0.07
Nodes (26): 1.1 Last Active shows wrong time (timezone), 1.2 Last Active is period-scoped, 1.3 Period boundaries are UTC, 1.4 Traffic-to-key join is broken and slow, 1.5 Key list has no paging, sorting, search, or Last Active column, 1. Problems, 2. Scope, 3.1 Timestamp normalization (+18 more)

### Community 16 - "Key Listing Implementation Plan"
Cohesion: 0.08
Nodes (25): API Key Listing & Last Active Fix — Implementation Plan, Deviations from the spec (intentional, decided while prototyping), File Map, Global Constraints, Phase 1 — Time Correctness, Phase 2 — Key Identity and Listing, Spec Coverage, Task 10: Proxy records `api_key_id` (+17 more)

### Community 17 - "User Authentication & Sessions"
Cohesion: 0.13
Nodes (4): ContextWithUser(), Manager, User, contextWithUser()

### Community 18 - "Web Plugin Helpers & Tests"
Cohesion: 0.23
Nodes (18): formatInheritText(), scopeLabel(), scopeSubLabel(), sortPipeline(), warningBannerText(), mount(), openDrawer(), renderOverrides() (+10 more)

### Community 19 - "Web Frontend API Client"
Cohesion: 0.12
Nodes (15): ApiError, qs(), redirectToLogin(), request(), url(), withTZ(), appendAll(), nanoOf() (+7 more)

### Community 20 - "Database Schema Migrations & Seeding"
Cohesion: 0.15
Nodes (17): DB, columnExists(), indexExists(), TestTrafficKeyIDColumnAndIndexes(), addKey(), addTraffic(), ids(), idsStr() (+9 more)

### Community 21 - "Web Dashboard Charting Components"
Cohesion: 0.24
Nodes (20): axisGrid(), fmtPct(), frame(), keyNav(), lineChart(), draw(), show(), niceTicks() (+12 more)

### Community 22 - "UI Popover & Select Dropdowns"
Cohesion: 0.26
Nodes (20): highlightMatches(), positionPopover(), searchableSelect(), close(), createPopover(), findOption(), getFilteredOptions(), moveFocus() (+12 more)

### Community 23 - "Plugin Domain Types & Metadata"
Cohesion: 0.18
Nodes (13): PluginWithMeta, Guidance, ScopeLabel(), Binding, Plugin, BindingState, Category, FailurePolicy (+5 more)

### Community 24 - "Auth Login & Reset Pages"
Cohesion: 0.40
Nodes (14): busy(), card, field(), nextURL(), params, post(), renderLogin(), renderRecovery() (+6 more)

### Community 25 - "Built-in Caveman Style Plugin"
Cohesion: 0.19
Nodes (12): ApplyCaveman(), TestApplyCaveman_DeveloperRoleMarkerDetection(), TestApplyCaveman_InjectionAndIdempotency(), TestApplyCaveman_PromptOverride(), EstimateOverhead(), HasMarker(), ApplyPonytail(), TestApplyPonytail_InjectionAndIdempotency() (+4 more)

### Community 26 - "HTTP Plugin Protocol & Pipeline"
Cohesion: 0.15
Nodes (12): CallHTTPPlugin(), ComputeAppliedHeader(), parseIncomingApplied(), toAnySlice(), toMapSlice(), MatchModelPattern(), mergeJSON(), ResolvePluginsForRequest() (+4 more)

### Community 28 - "Model Group Access Rules"
Cohesion: 0.36
Nodes (5): generateGroupID(), Manager, ModelGroup, ParseJSONStringArray(), SerializeJSONStringArray()

### Community 29 - "User Profile Dashboard View"
Cohesion: 0.38
Nodes (11): setUser(), fmtAgo(), mount(), accountCard(), load(), passwordCard(), recoveryCard(), updateCardContent() (+3 more)

### Community 30 - "Platform Build & Deployment Guide"
Cohesion: 0.18
Nodes (11): 1. Build Langsung di Windows (PowerShell / Command Prompt), 2. Cross-Compile untuk Windows dari Linux / macOS, 3. Mode Eksekusi CLI di Windows, Cara 1: Menggunakan Binary Go (Linux & macOS), Cara 2: Build & Menjalankan di Windows, Cara 3: Menggunakan Docker, Docker Compose (NineGuard + 9router), Instalasi & Menjalankan (+3 more)

### Community 31 - "System Overview & Feature Docs"
Cohesion: 0.18
Nodes (11): Arsitektur: Bagaimana NineGuard Bekerja?, Dokumentasi Terkait, Fitur Utama, Gambaran Umum (Overview), Konfigurasi Lingkungan (Environment Variables), Lisensi, NineGuard, NineGuard Management API (Dashboard & Admin) (+3 more)

### Community 32 - "Plugin Execution Engine Tests"
Cohesion: 0.24
Nodes (9): NewManager(), TestManager_CRUDAndCache(), TestManager_UpdatePipelineOrder(), NewPipelineExecutor(), TestPipelineExecution_BypassAndIdempotency(), TestPipelineExecution_ExplicitReject(), TestPipelineExecution_FailOpenAndClosed(), TestPipelineExecution_OrderAndTransformation() (+1 more)

### Community 33 - "API Key Listing Paging & Limits"
Cohesion: 0.25
Nodes (7): ClampLimit(), KeyPage, ListOptions, Manager, likeEscape(), oneOf(), TestClampLimit()

### Community 34 - "Multi-Target Build Scripts"
Cohesion: 0.56
Nodes (8): build_all(), build_auto(), build_desktop(), build_docker(), build_server(), build_windows(), detect_environment(), build.sh script

### Community 35 - "IDE & Agent Integration Guide"
Cohesion: 0.25
Nodes (8): 1. Cursor IDE, 2. Cline / Roo Code (VS Code Extension), 3. Continue.dev, 4. Pi Coding Agent Harness, 5. Python (Official OpenAI SDK), 6. Node.js / TypeScript (Official OpenAI SDK), 7. cURL / Terminal Shell, Panduan Menghubungkan AI Coding Agents

### Community 36 - "High-Level System Architecture"
Cohesion: 0.25
Nodes (5): 1. Konsep Desain, 2. Alur Request Masuk (Request Lifecycle), 3. Komponen Inti, 4. Keamanan & Isolasi, Arsitektur NineGuard

### Community 37 - "Model Group Pattern Matching"
Cohesion: 0.29
Nodes (7): Aturan Pola Model, Contoh Alur via Dashboard, Contoh via REST API, Mode Akses API Key, Model Groups (Template Akses Model), Perilaku Penting, Respons Saat Ditolak

### Community 38 - "Environment Config Loader"
Cohesion: 0.48
Nodes (5): Config, getEnv(), getEnvBool(), loadDotEnv(), LoadFromEnv()

### Community 39 - "Plugin Conflict & Overlap Warnings"
Cohesion: 0.33
Nodes (6): Warning, ComputeWarnings(), TestComputeWarnings_InactivePluginsIgnored(), TestComputeWarnings_InputAndOutputNoOverlap(), TestComputeWarnings_OutputStyleOverlap(), TestComputeWarnings_UpstreamTokenSaving()

### Community 40 - "Upstream Provider Routing Guide"
Cohesion: 0.33
Nodes (5): 1. Menambahkan Upstream Provider Baru, 2. Bagaimana Prefix Routing Bekerja?, 3. Sinkronisasi Model (Manual & Otomatis), Contoh Panggilan Model dari Client:, Panduan Manajemen Upstream Providers

### Community 41 - "ADR-0001 HTTP Plugins Architecture"
Cohesion: 0.40
Nodes (4): 0001. Third-party plugins are HTTP services, Consequences, Context, Decision

### Community 42 - "ADR-0002 Model Group Dual Role"
Cohesion: 0.40
Nodes (4): 0002. Model Group serves as both access template and plugin scope, Consequences, Context, Decision

### Community 43 - "ADR-0003 Key ID Traffic Linking"
Cohesion: 0.40
Nodes (4): 0003. Traffic is linked to API Keys by ID, Consequences, Context, Decision

### Community 44 - "ADR-0004 Sparse Plugin Bindings"
Cohesion: 0.40
Nodes (4): 0004. Sparse plugin bindings and overrides model, Consequences, Context, Decision

### Community 45 - "System Glossary & Terminology"
Cohesion: 0.40
Nodes (4): Access, NineGuard Glossary, Plugins, Reporting

### Community 46 - "Quick Setup Guide For Agents"
Cohesion: 0.40
Nodes (5): 1. Daftarkan Upstream Provider, 2. Generate NineGuard API Key untuk Agent, 3. Sinkronisasi Model & Pengujian (Opsional tapi Direkomendasikan), 4. Konfigurasikan pada AI Agent / IDE, Panduan Pengaturan Cepat

### Community 48 - "Author & Contributor Metadata"
Cohesion: 0.67
Nodes (3): Author, Author & Kontributor, Kontributor

## Knowledge Gaps
- **183 isolated node(s):** `nineguard`, `contextKey`, `Manager`, `HTTPPluginResponse`, `chatRequest` (+178 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 270 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `h()` connect `Dashboard UI Helpers & Routing` to `Web Dashboard Query Filters`, `Web API Key Listing View`, `Web App Shell & Navigation`, `Web Dialogs & Modals`, `Web Plugin Helpers & Tests`, `Web Frontend API Client`, `Web Dashboard Charting Components`, `UI Popover & Select Dropdowns`, `Auth Login & Reset Pages`, `User Profile Dashboard View`?**
  _High betweenness centrality (0.033) - this node is a cross-community bridge._
- **Why does `Handler` connect `HTTP API & Auth Handlers` to `Core Server & Runtime Config`, `Database & Query Logging`, `Server Main & CLI Entrypoint`, `Slog Logger & Concurrency Control`, `API Key Security & Hashing`, `User Authentication & Sessions`, `Plugin Manager CRUD & Lifecycle`?**
  _High betweenness centrality (0.032) - this node is a cross-community bridge._
- **Why does `main()` connect `Server Main & CLI Entrypoint` to `Core Server & Runtime Config`, `HTTP API & Auth Handlers`, `Plugin Execution Engine Tests`, `Slog Logger & Concurrency Control`, `Environment Config Loader`, `Terminal Input & Browser Launcher`, `Plugin & Headroom Unit Tests`?**
  _High betweenness centrality (0.021) - this node is a cross-community bridge._
- **What connects `nineguard`, `contextKey`, `Manager` to the rest of the system?**
  _183 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Core Server & Runtime Config` be split into smaller, more focused modules?**
  _Cohesion score 0.09045340624287992 - nodes in this community are weakly interconnected._
- **Should `HTTP API & Auth Handlers` be split into smaller, more focused modules?**
  _Cohesion score 0.12887112887112886 - nodes in this community are weakly interconnected._
- **Should `Database & Query Logging` be split into smaller, more focused modules?**
  _Cohesion score 0.05664568678267309 - nodes in this community are weakly interconnected._