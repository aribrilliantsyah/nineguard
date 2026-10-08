# Plugin System — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform chat completion requests through token-saving plugins (Caveman, Ponytail, Headroom) and HTTP plugins, configured globally, per model group, or per API key with strict precedence, idempotency, telemetry, and overlap warnings.

**Architecture:** A new package `internal/plugins` manages plugin definitions, bindings, scope resolution, overlap warnings, and pipeline execution. Handlers expose REST APIs for plugin registration, bindings, secret rotation, and resolution previews. The proxy pipeline intercepts `/v1/chat/completions` after auth/ACL checks, applies resolved plugins in global pipeline order, passes idempotency headers upstream, and records tokens saved, overhead, and plugin errors in `traffic_logs`. Web views expose configuration and telemetry.

**Tech Stack:** Go 1.27.1, `modernc.org/sqlite` (pure Go), `net/http`, vanilla ES modules (no build step), `node --test` for JS helpers.

**Spec:** `docs/superpowers/specs/2026-10-06-plugin-system-design.md` (ADRs: `docs/adr/0001-http-plugins.md`, `docs/adr/0002-model-group-dual-role.md`, `docs/adr/0003-traffic-linked-by-key-id.md`)

---

## Global Constraints

- Go `1.27.1`; all builds and tests must pass `CGO_ENABLED=0 go test -tags server ./...`. No new external Go dependencies.
- Every API timestamp format remains RFC 3339 UTC ending in `Z`.
- Client IP is never sent to plugins (`internal/plugins/http.go`).
- Plugins never modify the model; if an external plugin modifies `model`, NineGuard restores the original value and logs a warning.
- `TokensSaved` is only recorded from plugin-reported values (Headroom or HTTP response). Never estimated.
- `TokensOverhead` is recorded from plugin-reported overhead or estimated as `ceil(len(injected_text) / 4)` for Caveman/Ponytail.
- Prompt markers `[nineguard:caveman]` and `[nineguard:ponytail]` must be at index 0 on the first line of injected system messages.
- `X-NineGuard-Plugins` header (bypass request) is never forwarded upstream.
- `X-NineGuard-Plugins-Applied` is populated with sorted, de-duplicated union of built-in IDs and sent upstream.
- JS test files live in `web/jstest/` (never under `web/static/`).
- Never stage `package.json` or `package-lock.json`. Stage files by explicit paths only.

---

## File Map

| File | Phase | Responsibility |
|---|---|---|
| `NOTICE` | 1 | Attribution for Caveman (Apache-2.0) and Ponytail (MIT) prompts |
| `internal/plugins/builtin/prompts/embed.go` | 1 | Embedded prompt texts for Caveman variants and Ponytail levels |
| `internal/plugins/types.go` | 1 | Domain structs: Plugin, Binding, Scope, Category, Result, Warning |
| `internal/plugins/guidance.go` | 1 | Static guidance text constants for built-in plugins |
| `internal/db/db.go` | 1 | Database migrations for `plugins`, `plugin_bindings`, schema additions, and startup seeding |
| `internal/models/groups.go` | 1 | Add `priority` field to ModelGroup struct and CRUD operations |
| `internal/providers/providers.go` | 1 | Add `upstream_token_saving` and note fields to Provider struct and queries |
| `internal/plugins/builtin/caveman.go` | 2 | Caveman transformer, marker check, overhead estimation |
| `internal/plugins/builtin/ponytail.go` | 2 | Ponytail transformer, marker check, overhead estimation |
| `internal/plugins/builtin/headroom.go` | 2 | Headroom HTTP connector, `frozen_message_count` computation, skipped check |
| `internal/plugins/resolve.go` | 2 | Scope resolution precedence, settings merge, scope label helpers |
| `internal/plugins/warnings.go` | 2 | Overlap warnings calculation (`output_style_overlap`, `upstream_token_saving`) |
| `internal/plugins/http.go` | 2 | Third-party HTTP plugin caller with auth header and timeout handling |
| `internal/plugins/pipeline.go` | 2 | Pipeline execution loop, ordering, fail-open/fail-closed policies, bypass check |
| `internal/plugins/manager.go` | 2 | Manager coordinating DB operations, in-memory cache, and pipeline execution |
| `internal/traffic/traffic.go` | 3 | Schema mapping for plugin telemetry in `traffic_logs`, reporting summaries |
| `internal/proxy/proxy.go` | 3 | Proxy chat completion hook, plugin execution, header propagation, log recording |
| `internal/handler/plugins.go` | 4 | HTTP handlers for `/api/v1/plugins` endpoints, audit logging, RBAC checks |
| `internal/handler/handler.go` | 4 | Wire plugin manager into core handler struct |
| `cmd/nineguard/main.go` | 4 | Initialize plugin manager, register routes |
| `web/static/js/pluginhelpers.js` | 5 | Pure JS helpers for scope labelling, warning formatting, and toggle state |
| `web/jstest/pluginhelpers.test.mjs` | 5 | `node --test` suite for plugin helpers |
| `web/static/js/views/plugins.js` | 5 | Plugins view: list, drag-reorder, drawer, bindings table, test button, resolve preview |
| `web/static/js/app.js` | 5 | Register Plugins navigation route in sidebar under Gateway |
| `web/static/js/views/models.js` | 5 | Model Groups table priority field editor and display |
| `web/static/js/views/providers.js` | 5 | Providers upstream token saving checkbox and badge |
| `web/static/js/views/endpoints.js` | 5 | Per-key plugins section linking to bindings |
| `web/static/js/views/traffic.js` | 5 | Traffic log modal plugin fields (plugins applied, tokens saved, plugin ms) |
| `web/static/js/views/dashboard.js` | 5 | Dashboard KPI card for Tokens Saved |
| `web/static/js/views/reports.js` | 5 | Usage report breakdown for Tokens Saved per key and per plugin |

---

# Phase 1 — Schema, Types, Seed Data & Model/Provider Extensions

### Task 1: Attribution `NOTICE` & Bundled Prompts

**Files:**
- Create: `NOTICE`
- Create: `internal/plugins/builtin/prompts/caveman.txt`
- Create: `internal/plugins/builtin/prompts/ultracave.txt`
- Create: `internal/plugins/builtin/prompts/megacave.txt`
- Create: `internal/plugins/builtin/prompts/ponytail.txt`
- Create: `internal/plugins/builtin/prompts/embed.go`
- Test: `internal/plugins/builtin/prompts/embed_test.go`

**Interfaces:**
- Produces:
  ```go
  package prompts
  var CavemanPrompt string
  var UltraCavePrompt string
  var MegaCavePrompt string
  var PonytailPrompt string
  ```

- [x] **Step 1: Create `NOTICE` file** with licenses for upstream components:
```text
NineGuard
Copyright 2026 NineGuard Contributors

This product includes software developed by third parties:

Caveman
Copyright 2026 Julius Brussee
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0

Ponytail
Copyright 2026 DietrichGebert
Licensed under the MIT License.
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.
```

- [x] **Step 2: Create prompt text files** under `internal/plugins/builtin/prompts/`:
  - `caveman.txt`:
```text
Respond like terse caveman. All technical substance stay exact, only fluff die. Drop: articles (a/an/the), filler (just/really/basically/actually/simply), pleasantries, hedging. Fragments OK. Short synonyms (big not extensive, fix not implement a solution for). Pattern: [thing] [action] [reason]. [next step]. Code blocks, file paths, commands, errors, URLs: keep exact. Multi-step ordered sequences: write normal.
```
  - `ultracave.txt`:
```text
Ultra terse caveman mode. Max compression. Drop all articles, pleasantries, filler, hedging. Syntax fragments only. Technical names, commands, paths, code: exact. Pattern: [thing] [action]. No intro, no outro.
```
  - `megacave.txt`:
```text
文言文極簡模式。凡問答悉以文言，言簡意賅，字無贅疣。名物、指令、程式、路徑、代碼悉依原文，毋得妄易。直陳要害，斷絕虛文。
```
  - `ponytail.txt`:
```text
Provide concise, dense technical responses without preamble or conversational filler. Keep code examples minimal, focused, and directly addressing the user request. Prefer bullet points for multi-step answers.
```

- [x] **Step 3: Write `embed.go` and failing test `embed_test.go`**:
  - `internal/plugins/builtin/prompts/embed.go`:
```go
package prompts

import _ "embed"

//go:embed caveman.txt
var CavemanPrompt string

//go:embed ultracave.txt
var UltraCavePrompt string

//go:embed megacave.txt
var MegaCavePrompt string

//go:embed ponytail.txt
var PonytailPrompt string
```
  - `internal/plugins/builtin/prompts/embed_test.go`:
```go
package prompts

import (
	"strings"
	"testing"
)

func TestEmbeddedPrompts(t *testing.T) {
	if !strings.Contains(CavemanPrompt, "terse caveman") {
		t.Errorf("expected CavemanPrompt to contain 'terse caveman', got %q", CavemanPrompt)
	}
	if !strings.Contains(UltraCavePrompt, "Ultra terse") {
		t.Errorf("expected UltraCavePrompt to contain 'Ultra terse', got %q", UltraCavePrompt)
	}
	if !strings.Contains(MegaCavePrompt, "文言文") {
		t.Errorf("expected MegaCavePrompt to contain classical Chinese marker, got %q", MegaCavePrompt)
	}
	if !strings.Contains(PonytailPrompt, "concise") {
		t.Errorf("expected PonytailPrompt to contain 'concise', got %q", PonytailPrompt)
	}
}
```

- [x] **Step 4: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/builtin/prompts/...
```

---

### Task 2: Core Domain Types & Static Guidance

**Files:**
- Create: `internal/plugins/types.go`
- Create: `internal/plugins/guidance.go`
- Test: `internal/plugins/types_test.go`

**Interfaces:**
- `types.go`:
```go
package plugins

import "time"

type ScopeType string
const (
	ScopeGlobal ScopeType = "global"
	ScopeGroup  ScopeType = "group"
	ScopeKey    ScopeType = "key"
)

type BindingState string
const (
	StateOn      BindingState = "on"
	StateOff     BindingState = "off"
	StateInherit BindingState = "inherit"
)

type Category string
const (
	CategoryInputCompression Category = "input_compression"
	CategoryOutputStyle      Category = "output_style"
	CategoryOther            Category = "other"
)

type FailurePolicy string
const (
	PolicyOpen   FailurePolicy = "open"
	PolicyClosed FailurePolicy = "closed"
)

type PluginKind string
const (
	KindBuiltin PluginKind = "builtin"
	KindHTTP    PluginKind = "http"
)

type Plugin struct {
	ID              string        `json:"id"`
	Kind            PluginKind    `json:"kind"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	URL             string        `json:"url,omitempty"`
	Secret          string        `json:"secret,omitempty"`
	TimeoutMs       int           `json:"timeout_ms"`
	FailurePolicy   FailurePolicy `json:"failure_policy"`
	Bypassable      bool          `json:"bypassable"`
	PipelineOrder   int           `json:"pipeline_order"`
	Category        Category      `json:"category"`
	Summary         string        `json:"summary,omitempty"`
	DefaultSettings string        `json:"default_settings"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type Binding struct {
	PluginID  string       `json:"plugin_id"`
	ScopeType ScopeType    `json:"scope_type"`
	ScopeID   string       `json:"scope_id"`
	State     BindingState `json:"state"`
	Settings  string       `json:"settings"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type ScopeOrigin struct {
	ScopeType ScopeType `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	Label     string    `json:"label"`
}

type Warning struct {
	Code     string   `json:"code"`
	Plugins  []string `json:"plugins"`
	Provider string   `json:"provider,omitempty"`
	Message  string   `json:"message"`
}

type ResolvedPlugin struct {
	Plugin          Plugin            `json:"plugin"`
	EffectiveState  BindingState      `json:"effective_state"`
	MergedSettings  map[string]any    `json:"merged_settings"`
	DecidedBy       ScopeOrigin       `json:"decided_by"`
	Overridden      []ScopeOrigin     `json:"overridden,omitempty"`
}

type PipelineResult struct {
	Body           []byte
	PluginsApplied []string
	PluginsSkipped []string // format id:reason
	TokensSaved    int
	TokensOverhead int
	PluginErrors   []string
	DurationMs     int64
	Rejected       bool
	RejectCode     int
	RejectMessage  string
}
```

- `guidance.go`:
```go
package plugins

type Guidance struct {
	Summary           string `json:"summary"`
	RecommendedFor    string `json:"recommended_for"`
	NotRecommendedFor string `json:"not_recommended_for"`
}

var BuiltinGuidance = map[string]Guidance{
	"headroom": {
		Summary:           "Compresses older messages and tool outputs before they are sent. Reduces input tokens; does not change answer style.",
		RecommendedFor:    "Long agent sessions, large tool outputs, expensive models.",
		NotRecommendedFor: "Short chats (little to compress); providers that already compress input.",
	},
	"caveman": {
		Summary:           "Instructs the model to answer tersely, dropping filler. Reduces output tokens.",
		RecommendedFor:    "Coding agents, CLI tools.",
		NotRecommendedFor: "Roleplay, creative writing, teaching/explanations; combining with Ponytail.",
	},
	"ponytail": {
		Summary:           "Instructs the model to answer compactly at a chosen level (lite/full/ultra). Reduces output tokens.",
		RecommendedFor:    "Coding agents wanting a milder style than Caveman (lite).",
		NotRecommendedFor: "Roleplay, creative writing; combining with Caveman.",
	},
}
```

- [x] **Step 1: Write `types_test.go`** verifying types and guidance:
```go
package plugins

import "testing"

func TestGuidancePresence(t *testing.T) {
	for _, id := range []string{"headroom", "caveman", "ponytail"} {
		g, ok := BuiltinGuidance[id]
		if !ok || g.Summary == "" {
			t.Errorf("missing guidance for built-in %s", id)
		}
	}
}
```

- [x] **Step 2: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/...
```

---

### Task 3: Database Migrations & Built-in Seeding

**Files:**
- Modify: `internal/db/db.go`
- Test: `internal/db/db_test.go`

**Interfaces:**
- Tables created: `plugins`, `plugin_bindings`
- Columns added if not present:
  - `model_groups.priority INTEGER DEFAULT 0`
  - `providers.upstream_token_saving INTEGER DEFAULT 0`
  - `providers.upstream_token_saving_note TEXT DEFAULT ''`
  - `traffic_logs.plugins_skipped TEXT DEFAULT ''`
  - `traffic_logs.plugins_applied TEXT DEFAULT ''`
  - `traffic_logs.tokens_saved INTEGER DEFAULT 0`
  - `traffic_logs.tokens_overhead INTEGER DEFAULT 0`
  - `traffic_logs.plugin_errors TEXT DEFAULT ''`
  - `traffic_logs.plugin_ms INTEGER DEFAULT 0`
- Startup seed: inserts built-in plugins (`headroom`, `ponytail`, `caveman`) and global `off` bindings idempotently.

- [x] **Step 1: Write failing test in `internal/db/db_test.go`** verifying plugin tables and columns exist:
```go
func TestPluginMigrationsAndSeeding(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_plugins.db")
	database, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Verify tables
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM plugins").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query plugins table: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected at least 3 seeded plugins, got %d", count)
	}

	// Verify global bindings
	err = database.QueryRow("SELECT COUNT(*) FROM plugin_bindings WHERE scope_type = 'global' AND state = 'off'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query plugin_bindings: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected 3 global off bindings, got %d", count)
	}

	// Verify model_groups priority column
	var priority int
	err = database.QueryRow("SELECT priority FROM model_groups LIMIT 1").Scan(&priority)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("column priority missing on model_groups: %v", err)
	}
}
```

- [x] **Step 2: Update `internal/db/db.go`** to run migrations and seed built-in plugins:
  - Add `plugins` and `plugin_bindings` to `initSchema()`.
  - Add schema alterations via `addColumnIfNotExists`:
    ```go
    addColumnIfNotExists(db, "model_groups", "priority", "INTEGER DEFAULT 0")
    addColumnIfNotExists(db, "providers", "upstream_token_saving", "INTEGER DEFAULT 0")
    addColumnIfNotExists(db, "providers", "upstream_token_saving_note", "TEXT DEFAULT ''")
    addColumnIfNotExists(db, "traffic_logs", "plugins_skipped", "TEXT DEFAULT ''")
    addColumnIfNotExists(db, "traffic_logs", "plugins_applied", "TEXT DEFAULT ''")
    addColumnIfNotExists(db, "traffic_logs", "tokens_saved", "INTEGER DEFAULT 0")
    addColumnIfNotExists(db, "traffic_logs", "tokens_overhead", "INTEGER DEFAULT 0")
    addColumnIfNotExists(db, "traffic_logs", "plugin_errors", "TEXT DEFAULT ''")
    addColumnIfNotExists(db, "traffic_logs", "plugin_ms", "INTEGER DEFAULT 0")
    ```
  - Call `seedPlugins(db)` in `New()`:
    ```go
    func seedPlugins(db *sql.DB) {
        builtins := []struct {
            id, name, desc, category string
            order                    int
            settings                 string
        }{
            {"headroom", "Headroom", "Compresses message history before forwarding to model", "input_compression", 10, `{"url":"http://127.0.0.1:8787","mode":"incremental","compress_user_messages":false}`},
            {"ponytail", "Ponytail", "Instructs model to respond with compact formatting", "output_style", 20, `{"level":"full"}`},
            {"caveman", "Caveman", "Instructs model to respond in terse caveman style", "output_style", 30, `{"variant":"caveman"}`},
        }
        for _, b := range builtins {
            _, _ = db.Exec(`
                INSERT INTO plugins (id, kind, name, description, category, bypassable, pipeline_order, failure_policy, default_settings, created_at, updated_at)
                VALUES (?, 'builtin', ?, ?, ?, 1, ?, 'open', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
                ON CONFLICT(id) DO UPDATE SET
                    name = excluded.name,
                    category = excluded.category,
                    pipeline_order = excluded.pipeline_order,
                    default_settings = excluded.default_settings
            `, b.id, b.name, b.desc, b.category, b.order, b.settings)

            _, _ = db.Exec(`
                INSERT INTO plugin_bindings (plugin_id, scope_type, scope_id, state, settings, updated_at)
                VALUES (?, 'global', '', 'off', '{}', CURRENT_TIMESTAMP)
                ON CONFLICT(plugin_id, scope_type, scope_id) DO NOTHING
            `, b.id)
        }
    }
    ```

- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/db/...
```

---

### Task 4: Model Groups Priority & Providers Token-Saving Configuration

**Files:**
- Modify: `internal/models/groups.go`
- Modify: `internal/providers/providers.go`
- Test: `internal/models/groups_test.go`
- Test: `internal/providers/providers_test.go`

**Interfaces:**
- `ModelGroup` gains `Priority int json:"priority"`
- `Manager.CreateGroup` and `Manager.UpdateGroup` accept `priority int` (or update methods handle it)
- `Provider` gains `UpstreamTokenSaving bool json:"upstream_token_saving"`, `UpstreamTokenSavingNote string json:"upstream_token_saving_note"`
- `Manager.CreateProvider` and `Manager.UpdateProvider` accept the two fields.

- [x] **Step 1: Write failing test in `internal/models/groups_test.go`**:
```go
func TestGroupPriority(t *testing.T) {
	db := setupTestDB(t)
	m := NewManager(db)

	grp, err := m.CreateGroupWithPriority("HighPri", "desc", []string{"gpt-4"}, 10)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if grp.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", grp.Priority)
	}

	got, err := m.GetGroup(grp.ID)
	if err != nil || got.Priority != 10 {
		t.Fatalf("expected retrieved priority 10, got %d", got.Priority)
	}
}
```

- [x] **Step 2: Update `internal/models/groups.go`**:
  - Add `Priority int` to `ModelGroup` struct.
  - Update SELECT queries in `ListGroups` and `GetGroup` to fetch `COALESCE(priority, 0)`.
  - Add `CreateGroupWithPriority` and `UpdateGroupWithPriority` (or update existing methods while preserving backward compatibility with default 0).
  - Add cascade delete of bindings when group is deleted:
    ```go
    _, _ = m.db.Exec("DELETE FROM plugin_bindings WHERE scope_type = 'group' AND scope_id = ?", id)
    ```

- [x] **Step 3: Update `internal/providers/providers.go`**:
  - Add `UpstreamTokenSaving bool` and `UpstreamTokenSavingNote string` to `Provider` struct.
  - Update SELECT and INSERT/UPDATE statements in `ListProviders`, `GetProvider`, `CreateProvider`, and `UpdateProvider`.
  - Add tests in `internal/providers/providers_test.go` validating persistence and retrieval.

- [x] **Step 4: Update `internal/keys/keys.go`**:
  - Add cascade delete of key bindings in `DeleteKey(id string)`:
    ```go
    _, _ = m.db.Exec("DELETE FROM plugin_bindings WHERE scope_type = 'key' AND scope_id = ?", id)
    ```

- [x] **Step 5: Run tests**:
```bash
go test -v ./internal/models/... ./internal/providers/... ./internal/keys/...
```

---

# Phase 2 — Plugin Engine, Transformers, Resolution & Pipeline

### Task 5: Caveman and Ponytail Transformers

**Files:**
- Create: `internal/plugins/builtin/caveman.go`
- Create: `internal/plugins/builtin/ponytail.go`
- Test: `internal/plugins/builtin/caveman_test.go`
- Test: `internal/plugins/builtin/ponytail_test.go`

**Interfaces:**
- `caveman.go`:
  ```go
  func ApplyCaveman(messages []map[string]any, settings map[string]any) (newMessages []map[string]any, skipped bool, skipReason string, overhead int, err error)
  ```
  - Markers: `[nineguard:caveman]\n`
  - Injects at index 0. If marker present in system or developer message (string or content array), returns `skipped: true, skipReason: "marker_present", overhead: 0`.
  - Overhead: `(len(injectedText) + 3) / 4`.
  - Variants: `caveman`, `ultracave`, `megacave`. Supports `prompt_override`.

- `ponytail.go`:
  ```go
  func ApplyPonytail(messages []map[string]any, settings map[string]any) (newMessages []map[string]any, skipped bool, skipReason string, overhead int, err error)
  ```
  - Markers: `[nineguard:ponytail]\n`
  - Injects at index 0. Checks for marker in system/developer message.
  - Levels: `lite`, `full`, `ultra`. Supports `prompt_override`.

- [x] **Step 1: Write test `internal/plugins/builtin/caveman_test.go`**:
```go
package builtin

import (
	"strings"
	"testing"
)

func TestApplyCaveman_InjectionAndIdempotency(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "How do I reverse a string in Go?"},
	}
	out, skipped, _, overhead, err := ApplyCaveman(msgs, map[string]any{"variant": "caveman"})
	if err != nil || skipped {
		t.Fatalf("unexpected err or skip: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0]["role"] != "system" {
		t.Fatalf("expected system message at index 0")
	}
	sysContent, _ := out[0]["content"].(string)
	if !strings.HasPrefix(sysContent, "[nineguard:caveman]\n") {
		t.Fatalf("missing marker prefix: %s", sysContent)
	}
	if overhead <= 0 {
		t.Fatalf("expected positive overhead, got %d", overhead)
	}

	// Test idempotency when marker present
	out2, skipped2, reason, overhead2, err2 := ApplyCaveman(out, map[string]any{"variant": "caveman"})
	if err2 != nil || !skipped2 || reason != "marker_present" || overhead2 != 0 {
		t.Fatalf("expected idempotency skip, got skipped=%v, reason=%s, err=%v", skipped2, reason, err2)
	}
	if len(out2) != len(out) {
		t.Fatalf("expected message list unchanged on skip")
	}
}
```

- [x] **Step 2: Write test `internal/plugins/builtin/ponytail_test.go`** testing injection and marker detection.
- [x] **Step 3: Implement `caveman.go` and `ponytail.go`**.
- [x] **Step 4: Run tests to verify they pass**:
```bash
go test -v ./internal/plugins/builtin/...
```

---

### Task 6: Headroom Transformer

**Files:**
- Create: `internal/plugins/builtin/headroom.go`
- Test: `internal/plugins/builtin/headroom_test.go`

**Interfaces:**
- `headroom.go`:
  ```go
  func ApplyHeadroom(ctx context.Context, client *http.Client, reqBody map[string]any, settings map[string]any) (modifiedBody map[string]any, tokensSaved int, skipped bool, skipReason string, err error)
  ```
  - Mode: `incremental` (calculates `config.frozen_message_count` = index immediately following last `assistant` message) or `full` (`frozen_message_count = 0`).
  - Calls `POST {url}/v1/compress` with `{messages, model, config}`.
  - Passes optional `HEADROOM_PROXY_TOKEN` in `Authorization: Bearer <token>`.
  - Preserves original `system` and `tools` messages.
  - If Headroom returns `compression_skipped: true`, returns `skipped: false`, `tokensSaved: 0`.

- [x] **Step 1: Write test `internal/plugins/builtin/headroom_test.go`** using `httptest.Server`:
```go
package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyHeadroom_IncrementalAndPreservation(t *testing.T) {
	var receivedPayload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compress" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": "compressed user prompt"},
			},
			"tokens_saved": 42,
		})
	}))
	defer ts.Close()

	body := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "system", "content": "system instruction"},
			{"role": "user", "content": "original user prompt"},
			{"role": "assistant", "content": "assistant reply"},
			{"role": "user", "content": "second user prompt"},
		},
		"tools": []any{map[string]any{"type": "function"}},
	}

	settings := map[string]any{
		"url":  ts.URL,
		"mode": "incremental",
	}

	res, saved, skipped, _, err := ApplyHeadroom(context.Background(), ts.Client(), body, settings)
	if err != nil || skipped {
		t.Fatalf("headroom failed: %v", err)
	}
	if saved != 42 {
		t.Fatalf("expected 42 tokens saved, got %d", saved)
	}

	// Check frozen_message_count sent to headroom
	cfg, _ := receivedPayload["config"].(map[string]any)
	frozen, _ := cfg["frozen_message_count"].(float64)
	if int(frozen) != 3 { // index after assistant is 3
		t.Fatalf("expected frozen count 3, got %v", frozen)
	}

	// Verify tools preserved
	if res["tools"] == nil {
		t.Fatalf("tools should be preserved")
	}
}
```

- [x] **Step 2: Implement `headroom.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/builtin/...
```

---

### Task 7: Scope Resolution & Settings Merge

**Files:**
- Create: `internal/plugins/resolve.go`
- Test: `internal/plugins/resolve_test.go`

**Interfaces:**
- Produces:
  ```go
  func ScopeLabel(scopeType ScopeType, scopeID, name string) ScopeOrigin
  func ResolvePluginsForRequest(
      allPlugins []Plugin,
      allBindings []Binding,
      groups []models.ModelGroup,
      apiKeyID string,
      model string,
  ) []ResolvedPlugin
  ```
  - Priority logic: Global -> matching Model Groups (highest `priority`, ties broken by group name ascending) -> API Key.
  - Matches model patterns: exact, `*`, `prefix/*`, `*.suffix`, flexible provider prefix (parity with `keys.IsModelAllowed`).
  - Settings merge: `default_settings` <- global <- group <- key.
  - Populates `DecidedBy` and `Overridden` candidate groups.

- [x] **Step 1: Write test `internal/plugins/resolve_test.go`** covering spec §5.5 reference scenario:
```go
package plugins

import (
	"testing"
	"nineguard/internal/models"
)

func TestResolution_ReferenceScenario(t *testing.T) {
	// Setup reference fixtures from §5.5:
	// Groups: Coding, Roleplay, Demanding
	// Keys: pi-dev, marinara
	// Plugins: headroom, caveman, ponytail
	groups := []models.ModelGroup{
		{ID: "grp_coding", Name: "Coding", Priority: 0, Models: []string{"office/claude-sonnet-4", "office/gpt-5", "9r/claude-opus-4", "9r/qwen3-coder"}},
		{ID: "grp_roleplay", Name: "Roleplay", Priority: 0, Models: []string{"9r/claude-opus-4", "9r/deepseek-v3"}},
		{ID: "grp_demanding", Name: "Demanding", Priority: 10, Models: []string{"office/claude-sonnet-4", "9r/claude-opus-4"}},
	}
	pluginsList := []Plugin{
		{ID: "headroom", Kind: KindBuiltin, Category: CategoryInputCompression, PipelineOrder: 10, DefaultSettings: "{}"},
		{ID: "caveman", Kind: KindBuiltin, Category: CategoryOutputStyle, PipelineOrder: 30, DefaultSettings: "{}"},
		{ID: "ponytail", Kind: KindBuiltin, Category: CategoryOutputStyle, PipelineOrder: 20, DefaultSettings: "{}"},
	}
	bindings := []Binding{
		{PluginID: "headroom", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "caveman", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "ponytail", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "headroom", ScopeType: ScopeGroup, ScopeID: "grp_demanding", State: StateOn},
		{PluginID: "caveman", ScopeType: ScopeKey, ScopeID: "key_pidev", State: StateOn},
	}

	// pi-dev requesting office/claude-sonnet-4 -> Headroom and Caveman run
	resolved := ResolvePluginsForRequest(pluginsList, bindings, groups, "key_pidev", "office/claude-sonnet-4")
	var onPlugins []string
	for _, r := range resolved {
		if r.EffectiveState == StateOn {
			onPlugins = append(onPlugins, r.Plugin.ID)
		}
	}
	if len(onPlugins) != 2 || onPlugins[0] != "headroom" || onPlugins[1] != "caveman" {
		t.Fatalf("expected [headroom, caveman], got %v", onPlugins)
	}
}
```

- [x] **Step 2: Implement `internal/plugins/resolve.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/...
```

---

### Task 8: Overlap Warnings

**Files:**
- Create: `internal/plugins/warnings.go`
- Test: `internal/plugins/warnings_test.go`

**Interfaces:**
- Produces:
  ```go
  func ComputeWarnings(effective []ResolvedPlugin, providerIsTokenSaving bool, providerNote, providerName string) []Warning
  ```
  - `output_style_overlap`: 2 or more effective plugins with `CategoryOutputStyle`.
  - `upstream_token_saving`: 1 or more effective plugins with `CategoryInputCompression` or `CategoryOutputStyle`, when provider has `upstream_token_saving == true`.
  - Pure function; returns empty slice when no overlap.

- [x] **Step 1: Write test `internal/plugins/warnings_test.go`**:
```go
package plugins

import "testing"

func TestComputeWarnings(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "caveman", Name: "Caveman", Category: CategoryOutputStyle}, EffectiveState: StateOn},
		{Plugin: Plugin{ID: "ponytail", Name: "Ponytail", Category: CategoryOutputStyle}, EffectiveState: StateOn},
	}

	w := ComputeWarnings(effective, false, "", "")
	if len(w) != 1 || w[0].Code != "output_style_overlap" {
		t.Fatalf("expected output_style_overlap warning, got %v", w)
	}

	// Upstream warning
	w2 := ComputeWarnings([]ResolvedPlugin{effective[0]}, true, "RTK enabled", "9router")
	if len(w2) != 1 || w2[0].Code != "upstream_token_saving" {
		t.Fatalf("expected upstream_token_saving warning, got %v", w2)
	}
}
```

- [x] **Step 2: Implement `internal/plugins/warnings.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/...
```

---

### Task 9: HTTP Plugin Caller & Pipeline Execution

**Files:**
- Create: `internal/plugins/http.go`
- Create: `internal/plugins/pipeline.go`
- Test: `internal/plugins/pipeline_test.go`

**Interfaces:**
- `http.go`:
  - Dispatches POST request with header `X-NineGuard-Plugin-Secret`.
  - Strips client IP from context.
  - Validates response: restores original model if plugin changed it.
- `pipeline.go`:
  - `ExecutePipeline(ctx, client, plugins, bindings, groups, keyInfo, model, providerInfo, bypassHeader, incomingAppliedHeader, bodyBytes) (PipelineResult, error)`
  - Bypassing: if `bypassHeader == "off"`, skips plugins where `Bypassable == true`.
  - Pipeline order: runs in ascending `PipelineOrder`.
  - Idempotency: checks incoming `X-NineGuard-Plugins-Applied`. If built-in present in header and plugin is bypassable, skip with reason `already_applied`.
  - Policy handling: fail-open logs error and continues; fail-closed halts with 503; explicit reject halts with 403.
  - Telemetry: tracks `TokensSaved`, `TokensOverhead`, `PluginsApplied`, `PluginsSkipped`, `PluginErrors`, `PluginMs`.

- [x] **Step 1: Write test in `internal/plugins/pipeline_test.go`** testing ordering, fail-open, fail-closed, reject, bypass header, and idempotency header:
```go
package plugins

import (
	"context"
	"net/http"
	"testing"
)

func TestPipelineExecution_FailOpenAndClosed(t *testing.T) {
	// Setup test with failing open plugin and failing closed plugin
	// Validate 503 on closed, skip on open
}
```

- [x] **Step 2: Implement `internal/plugins/http.go` and `internal/plugins/pipeline.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/...
```

---

### Task 10: Plugin Manager & DB Repository

**Files:**
- Create: `internal/plugins/manager.go`
- Test: `internal/plugins/manager_test.go`

**Interfaces:**
- Produces:
  ```go
  type Manager struct { ... }
  func NewManager(db *db.DB, httpClient *http.Client) *Manager
  func (m *Manager) ReloadCache() error
  func (m *Manager) ListPlugins() ([]Plugin, error)
  func (m *Manager) GetPlugin(id string) (*Plugin, error)
  func (m *Manager) CreateHTTPPlugin(p *Plugin) (*Plugin, string, error) // returns raw secret once
  func (m *Manager) UpdatePlugin(p *Plugin) error
  func (m *Manager) RotateSecret(id string) (string, error)
  func (m *Manager) DeletePlugin(id string) error
  func (m *Manager) UpdatePipelineOrder(orderedIDs []string) error
  func (m *Manager) ListBindings(pluginID string) ([]Binding, error)
  func (m *Manager) UpsertBinding(b Binding) error
  func (m *Manager) ResetPromptOverride(pluginID string) error
  func (m *Manager) ExecutePipeline(...) (PipelineResult, error)
  ```
  - Thread-safe via `sync.RWMutex`.
  - In-memory cache for fast per-request resolution without DB queries.

- [x] **Step 1: Write test `internal/plugins/manager_test.go`** testing CRUD, secret masking, order update, and cache refresh:
```go
package plugins

import (
	"path/filepath"
	"testing"
	"nineguard/internal/db"
)

func TestManager_CRUDAndCache(t *testing.T) {
	tmpDir := t.TempDir()
	database, _ := db.New(filepath.Join(tmpDir, "plugins_mgr.db"))
	defer database.Close()

	mgr := NewManager(database, nil)
	plugins, err := mgr.ListPlugins()
	if err != nil || len(plugins) < 3 {
		t.Fatalf("expected at least 3 seeded plugins, got %d (err: %v)", len(plugins), err)
	}

	// Secret masking check
	for _, p := range plugins {
		if p.Secret != "" {
			t.Fatalf("plugin secret should be empty/masked on list")
		}
	}
}
```

- [x] **Step 2: Implement `internal/plugins/manager.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/plugins/...
```

---

# Phase 3 — Proxy Pipeline Integration & Traffic Telemetry

### Task 11: Traffic Telemetry Updates

**Files:**
- Modify: `internal/traffic/traffic.go`
- Test: `internal/traffic/traffic_test.go`

**Interfaces:**
- `LogEntry` gains:
  - `PluginsSkipped string json:"plugins_skipped"`
  - `PluginsApplied string json:"plugins_applied"`
  - `TokensSaved int json:"tokens_saved"`
  - `TokensOverhead int json:"tokens_overhead"`
  - `PluginErrors string json:"plugin_errors"`
  - `PluginMs int json:"plugin_ms"`
- `DashboardStats` gains `TokensSaved int json:"tokens_saved"`.
- `UsageReport` breakdown includes `TokensSaved int json:"tokens_saved"`.

- [x] **Step 1: Write test in `internal/traffic/traffic_test.go`** verifying insertion and querying of plugin columns:
```go
func TestTrafficLog_PluginTelemetry(t *testing.T) {
	db := setupTestDB(t)
	tm := NewManager(db)

	entry := &LogEntry{
		APIKey:         "sk-ng-test1234",
		APIKeyName:     "TestKey",
		Model:          "gpt-4o",
		StatusCode:     200,
		TokensSaved:    128,
		TokensOverhead: 15,
		PluginsApplied: "headroom,caveman",
		PluginsSkipped: "ponytail:marker_present",
		PluginErrors:   "",
		PluginMs:       45,
	}
	if err := tm.Record(entry); err != nil {
		t.Fatalf("failed to record entry: %v", err)
	}

	stats, err := tm.GetDashboardStats(FilterParams{Period: "all"})
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TokensSaved < 128 {
		t.Fatalf("expected at least 128 tokens saved in stats, got %d", stats.TokensSaved)
	}
}
```

- [x] **Step 2: Update `internal/traffic/traffic.go`** to store and select plugin columns.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/traffic/...
```

---

### Task 12: Proxy Pipeline Interception

**Files:**
- Modify: `internal/proxy/proxy.go`
- Test: `internal/proxy/proxy_test.go`

**Interfaces:**
- `NewProxy` accepts `*plugins.Manager`.
- In `ServeHTTP`:
  - Chat completions requests (`/v1/chat/completions` or `/chat/completions`) trigger `plugins.Manager.ExecutePipeline` after authentication and model firewall checks.
  - If pipeline rejects: returns 403 `plugin_rejected` or 503 `plugin_unavailable`, records log with `Level: ERROR`.
  - Replaces body with transformed body.
  - Strips incoming `X-NineGuard-Plugins`.
  - Replaces `X-NineGuard-Plugins-Applied` with sorted union of executed built-ins and incoming built-in IDs.
  - Attaches telemetry to `traffic.LogEntry`.

- [x] **Step 1: Write integration test in `internal/proxy/proxy_test.go`**:
```go
func TestProxy_PluginPipelineExecution(t *testing.T) {
	// Mock upstream server echoing request body and headers
	// Call proxy with chat request
	// Assert upstream received transformed body
	// Assert X-NineGuard-Plugins stripped
	// Assert X-NineGuard-Plugins-Applied set
	// Assert traffic_logs saved tokens_saved and plugins_applied
}
```

- [x] **Step 2: Implement proxy interception in `internal/proxy/proxy.go`**.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/proxy/...
```

---

# Phase 4 — REST API Endpoints & Route Wiring

### Task 13: Plugin API Handlers

**Files:**
- Create: `internal/handler/plugins.go`
- Modify: `internal/handler/handler.go`
- Test: `internal/handler/plugins_test.go`

**Interfaces:**
- Endpoints:
  - `GET /api/v1/plugins` (lists plugins with masked secrets, global binding, guidance)
  - `POST /api/v1/plugins` (admin only; registers HTTP plugin, returns secret once, audits `plugin.create`)
  - `PUT /api/v1/plugins/{id}` (admin for url/secret/failure_policy; operator for rest; audits `plugin.update`)
  - `POST /api/v1/plugins/{id}/rotate-secret` (admin only; returns secret once, audits `plugin.rotate_secret`)
  - `DELETE /api/v1/plugins/{id}` (admin only; rejects built-ins; deletes HTTP plugin, audits `plugin.delete`)
  - `POST /api/v1/plugins/{id}/test` (sends test payload, returns latency and outcome)
  - `PUT /api/v1/plugins/order` (reorders pipeline, audits `plugin.order.update`)
  - `GET /api/v1/plugins/{id}/bindings` (lists all bindings for plugin)
  - `PUT /api/v1/plugins/{id}/bindings` (upserts binding, returns saved binding + scope warnings, audits `plugin.binding.update`)
  - `GET /api/v1/plugins/resolve?key_id=&model=` (previews effective plugins, settings, decided_by, overridden, warnings)
  - `GET /api/v1/plugins/warnings` (previews active warnings across active keys x enabled models up to 50k pairs)
  - `POST /api/v1/plugins/{id}/reset-prompt` (resets prompt_override to empty, audits `plugin.update`)

- [x] **Step 1: Write test `internal/handler/plugins_test.go`** verifying RBAC, secret masking, audit entries, and warnings response:
```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPluginHandlers_RBACAndListing(t *testing.T) {
	// Test GET /api/v1/plugins returns 200 with 3 builtins
	// Test POST /api/v1/plugins as operator returns 403 Forbidden
	// Test POST /api/v1/plugins as admin creates plugin and audits action
}
```

- [x] **Step 2: Implement `internal/handler/plugins.go`** and update `internal/handler/handler.go`.
- [x] **Step 3: Run test to verify it passes**:
```bash
go test -v ./internal/handler/...
```

---

### Task 14: Wiring in Main Server

**Files:**
- Modify: `cmd/nineguard/main.go`
- Test: `cmd/nineguard/security_e2e_test.go`

**Interfaces:**
- Initialize `plugins.NewManager(db, httpClient)`
- Pass `pluginsManager` to `proxy.NewProxy` and `handler.New`
- Register all plugin routes on `mux`.

- [x] **Step 1: Update `cmd/nineguard/main.go`** with plugin routes and wiring.
- [x] **Step 2: Update `cmd/nineguard/security_e2e_test.go`** to verify plugin routes are authenticated.
- [x] **Step 3: Run all server tests**:
```bash
go test ./cmd/nineguard/...
CGO_ENABLED=0 go test -tags server ./...
```

---

# Phase 5 — Frontend Dashboard & Management UI

### Task 15: Pure JS Helpers & Test Suite

**Files:**
- Create: `web/static/js/pluginhelpers.js`
- Create: `web/jstest/pluginhelpers.test.mjs`

**Interfaces:**
- Produces functions in `pluginhelpers.js`:
  ```javascript
  export function scopeLabel(scopeType, name)
  export function scopeSubLabel(scopeType, meta)
  export function formatInheritText(scopeType)
  export function warningBannerText(warnings)
  export function sortPipeline(plugins)
  ```

- [x] **Step 1: Write `web/jstest/pluginhelpers.test.mjs`**:
```javascript
import test from 'node:test';
import assert from 'node:assert/strict';
import { scopeLabel, scopeSubLabel, formatInheritText } from '../static/js/pluginhelpers.js';

test('scopeLabel conforms to spec §9.2', () => {
  assert.equal(scopeLabel('global'), 'All keys, all models');
  assert.equal(scopeLabel('group', 'Coding'), 'Models in "Coding"');
  assert.equal(scopeLabel('key', 'Cursor IDE'), 'Key "Cursor IDE"');
});

test('formatInheritText renders next broader scope', () => {
  assert.equal(formatInheritText('key'), 'Inherit (use group or All keys, all models)');
  assert.equal(formatInheritText('group'), 'Inherit (use All keys, all models)');
});
```

- [x] **Step 2: Implement `web/static/js/pluginhelpers.js`**.
- [x] **Step 3: Run node test to verify it passes**:
```bash
node --test web/jstest/pluginhelpers.test.mjs
```

---

### Task 16: Plugins View Page & Navigation

**Files:**
- Create: `web/static/js/views/plugins.js`
- Modify: `web/static/js/app.js`

**Interfaces:**
- New view `plugins.js`:
  - Upgrade banner: "Plugins available — all off by default."
  - Plugin list with drag-to-reorder, kind badges, bypassable status, failure policy, global toggle, test button.
  - Detail drawer:
    - Settings editor (typed fields for built-ins, JSON editor for HTTP plugins).
    - Prompt editor with "Reset to default" button for Caveman/Ponytail.
    - Bindings table with rows for Global, Model Groups, and API Keys; On/Off/Inherit select with clear labels.
  - "Register HTTP Plugin" modal (admin only) showing secret once with Copy button.
  - Resolve Preview tool: select key + model, display which plugins run, decided_by badge, overridden items, and warnings.
  - Built-in guidance card: summary, recommended for, not recommended for.
  - "Confirm on enable" modal with checklist and localStorage acknowledge checkbox (`ng.plugins.ack.{plugin_id}`).
- `app.js`:
  - Add `{ view: 'plugins', label: 'Plugins', icon: 'puzzle', keywords: 'plugins caveman ponytail headroom compress tokens transform' }` to Gateway nav group.

- [x] **Step 1: Implement `web/static/js/views/plugins.js`**.
- [x] **Step 2: Wire `plugins` view in `web/static/js/app.js`**.
- [x] **Step 3: Run node tests for web**:
```bash
node --test web/jstest/*.test.mjs
```

---

### Task 17: Model Groups, Providers & Endpoints View Updates

**Files:**
- Modify: `web/static/js/views/models.js`
- Modify: `web/static/js/views/providers.js`
- Modify: `web/static/js/views/endpoints.js`

**Interfaces:**
- `models.js`:
  - Add `Priority` column in Model Groups table and input field in group modal with help text.
- `providers.js`:
  - Add "Upstream already applies token saving" checkbox and note field to provider modal.
  - Display badge on flagged providers in provider list.
- `endpoints.js`:
  - Add per-key "Plugins" shortcut linking to bindings and resolve preview.

- [x] **Step 1: Update `models.js`, `providers.js`, and `endpoints.js`**.
- [x] **Step 2: Verify in browser / headless tests**.

---

### Task 18: Telemetry & Reporting View Updates

**Files:**
- Modify: `web/static/js/views/traffic.js`
- Modify: `web/static/js/views/dashboard.js`
- Modify: `web/static/js/views/reports.js`

**Interfaces:**
- `traffic.js`:
  - Columns and detail drawer fields for: Plugins Applied, Plugins Skipped, Tokens Saved, Overhead (est.), Plugin Errors, Plugin Latency.
- `dashboard.js`:
  - Add "Tokens Saved" KPI card with cumulative saved count.
- `reports.js`:
  - Add Tokens Saved breakdowns per API key and per plugin.

- [x] **Step 1: Update `traffic.js`, `dashboard.js`, and `reports.js`**.
- [x] **Step 2: Run all web tests**:
```bash
node --test web/jstest/*.test.mjs
```

---

# Phase 6 — Full Verification & Acceptance Checks

### Task 19: Full Test Suite & Build Verification

- [x] **Step 1: Run complete Go test suite**:
```bash
go test -v ./...
```
- [x] **Step 2: Run server tags build verification**:
```bash
CGO_ENABLED=0 go test -tags server ./...
```
- [x] **Step 3: Run Node.js test suite**:
```bash
node --test web/jstest/*.test.mjs
```
- [x] **Step 4: Check git status to ensure no stray untracked files like `package.json`**:
```bash
git status
```
