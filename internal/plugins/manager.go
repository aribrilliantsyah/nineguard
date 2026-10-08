package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/models"
)

type Manager struct {
	db       *db.DB
	executor *PipelineExecutor
	mu       sync.RWMutex
	plugins  []Plugin
	bindings []Binding
}

func NewManager(database *db.DB, httpClient *http.Client) *Manager {
	m := &Manager{
		db:       database,
		executor: NewPipelineExecutor(httpClient),
	}
	_ = m.ReloadCache()
	return m
}

func generateID(prefix string, byteLen int) string {
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// ReloadCache refreshes in-memory plugins and bindings.
func (m *Manager) ReloadCache() error {
	pRows, err := m.db.Query(`
		SELECT id, kind, name, COALESCE(description, ''), COALESCE(url, ''), COALESCE(secret, ''),
		       COALESCE(timeout_ms, 3000), failure_policy, bypassable, pipeline_order,
		       category, COALESCE(summary, ''), COALESCE(default_settings, '{}'),
		       created_at, updated_at
		FROM plugins
		ORDER BY pipeline_order ASC, id ASC
	`)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}
	defer pRows.Close()

	var pluginsList []Plugin
	for pRows.Next() {
		var p Plugin
		var bInt int
		if err := pRows.Scan(
			&p.ID, &p.Kind, &p.Name, &p.Description, &p.URL, &p.Secret,
			&p.TimeoutMs, &p.FailurePolicy, &bInt, &p.PipelineOrder,
			&p.Category, &p.Summary, &p.DefaultSettings,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return fmt.Errorf("scan plugin error: %w", err)
		}
		p.Bypassable = (bInt == 1)
		pluginsList = append(pluginsList, p)
	}

	bRows, err := m.db.Query(`
		SELECT plugin_id, scope_type, scope_id, state, COALESCE(settings, '{}'), updated_at
		FROM plugin_bindings
	`)
	if err != nil {
		return fmt.Errorf("failed to load bindings: %w", err)
	}
	defer bRows.Close()

	var bindingsList []Binding
	for bRows.Next() {
		var b Binding
		if err := bRows.Scan(&b.PluginID, &b.ScopeType, &b.ScopeID, &b.State, &b.Settings, &b.UpdatedAt); err != nil {
			return fmt.Errorf("scan binding error: %w", err)
		}
		bindingsList = append(bindingsList, b)
	}

	m.mu.Lock()
	m.plugins = pluginsList
	m.bindings = bindingsList
	m.mu.Unlock()

	return nil
}

// ListPlugins returns all plugins with secrets masked.
func (m *Manager) ListPlugins() ([]Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]Plugin, len(m.plugins))
	for i, p := range m.plugins {
		pCopy := p
		pCopy.Secret = ""
		res[i] = pCopy
	}
	return res, nil
}

// GetPlugin returns a plugin by ID with secret masked.
func (m *Manager) GetPlugin(id string) (*Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		if p.ID == id {
			pCopy := p
			pCopy.Secret = ""
			return &pCopy, nil
		}
	}
	return nil, fmt.Errorf("plugin %s not found", id)
}

// GetPluginRaw returns a plugin by ID with unmasked secret.
func (m *Manager) GetPluginRaw(id string) (*Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		if p.ID == id {
			pCopy := p
			return &pCopy, nil
		}
	}
	return nil, fmt.Errorf("plugin %s not found", id)
}

// CreateHTTPPlugin registers a new third-party HTTP plugin and returns the raw secret once.
func (m *Manager) CreateHTTPPlugin(p *Plugin) (*Plugin, string, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, "", fmt.Errorf("plugin name is required")
	}
	url := strings.TrimRight(strings.TrimSpace(p.URL), "/")
	if url == "" {
		return nil, "", fmt.Errorf("plugin URL is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, "", fmt.Errorf("plugin URL must use http or https")
	}

	id := generateID("plg_", 6)
	rawSecret := generateID("sec_", 16)

	timeoutMs := p.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	policy := p.FailurePolicy
	if policy == "" {
		policy = PolicyOpen
	}
	bypassable := p.Bypassable
	if policy == PolicyClosed {
		bypassable = false // Fail-closed plugins cannot be bypassable
	}

	cat := p.Category
	if cat == "" {
		cat = CategoryOther
	}

	defSettings := strings.TrimSpace(p.DefaultSettings)
	if defSettings == "" {
		defSettings = "{}"
	}

	tx, err := m.db.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	bInt := 0
	if bypassable {
		bInt = 1
	}

	// Max pipeline order + 10
	var maxOrder int
	_ = tx.QueryRow("SELECT COALESCE(MAX(pipeline_order), 90) FROM plugins").Scan(&maxOrder)
	newOrder := maxOrder + 10

	now := time.Now()
	_, err = tx.Exec(`
		INSERT INTO plugins (id, kind, name, description, url, secret, timeout_ms, failure_policy, bypassable, pipeline_order, category, summary, default_settings, created_at, updated_at)
		VALUES (?, 'http', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, name, strings.TrimSpace(p.Description), url, rawSecret, timeoutMs, policy, bInt, newOrder, cat, strings.TrimSpace(p.Summary), defSettings)
	if err != nil {
		return nil, "", fmt.Errorf("insert plugin failed: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO plugin_bindings (plugin_id, scope_type, scope_id, state, settings, updated_at)
		VALUES (?, 'global', '', 'off', '{}', CURRENT_TIMESTAMP)
	`, id)
	if err != nil {
		return nil, "", fmt.Errorf("insert global binding failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, "", err
	}

	_ = m.ReloadCache()

	return &Plugin{
		ID:              id,
		Kind:            KindHTTP,
		Name:            name,
		Description:     strings.TrimSpace(p.Description),
		URL:             url,
		TimeoutMs:       timeoutMs,
		FailurePolicy:   policy,
		Bypassable:      bypassable,
		PipelineOrder:   newOrder,
		Category:        cat,
		Summary:         strings.TrimSpace(p.Summary),
		DefaultSettings: defSettings,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, rawSecret, nil
}

// UpdatePlugin updates mutable plugin fields.
func (m *Manager) UpdatePlugin(p *Plugin) error {
	existing, err := m.GetPluginRaw(p.ID)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = existing.Name
	}
	desc := strings.TrimSpace(p.Description)
	url := strings.TrimRight(strings.TrimSpace(p.URL), "/")
	if existing.Kind == KindBuiltin {
		url = existing.URL // built-ins have fixed URLs
	} else if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("plugin URL must use http or https")
	}

	policy := p.FailurePolicy
	if policy == "" {
		policy = existing.FailurePolicy
	}
	bypassable := p.Bypassable
	if policy == PolicyClosed {
		bypassable = false
	}

	timeoutMs := p.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = existing.TimeoutMs
	}

	cat := p.Category
	if cat == "" {
		cat = existing.Category
	}

	defSettings := strings.TrimSpace(p.DefaultSettings)
	if defSettings == "" {
		defSettings = existing.DefaultSettings
	}

	bInt := 0
	if bypassable {
		bInt = 1
	}

	_, err = m.db.Exec(`
		UPDATE plugins
		SET name = ?, description = ?, url = ?, timeout_ms = ?, failure_policy = ?, bypassable = ?, category = ?, summary = ?, default_settings = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, desc, url, timeoutMs, policy, bInt, cat, strings.TrimSpace(p.Summary), defSettings, p.ID)
	if err != nil {
		return fmt.Errorf("update plugin error: %w", err)
	}

	_ = m.ReloadCache()
	return nil
}

// RotateSecret generates a new secret for an HTTP plugin and returns it once.
func (m *Manager) RotateSecret(id string) (string, error) {
	p, err := m.GetPluginRaw(id)
	if err != nil {
		return "", err
	}
	if p.Kind == KindBuiltin {
		return "", fmt.Errorf("cannot rotate secret for built-in plugin")
	}

	newSecret := generateID("sec_", 16)
	_, err = m.db.Exec(`
		UPDATE plugins SET secret = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, newSecret, id)
	if err != nil {
		return "", fmt.Errorf("failed to rotate secret: %w", err)
	}

	_ = m.ReloadCache()
	return newSecret, nil
}

// DeletePlugin deletes a third-party HTTP plugin.
func (m *Manager) DeletePlugin(id string) error {
	p, err := m.GetPluginRaw(id)
	if err != nil {
		return err
	}
	if p.Kind == KindBuiltin {
		return fmt.Errorf("built-in plugins cannot be deleted")
	}

	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM plugin_bindings WHERE plugin_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete bindings: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM plugins WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete plugin: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	_ = m.ReloadCache()
	return nil
}

// UpdatePipelineOrder updates the sequence of all plugins.
func (m *Manager) UpdatePipelineOrder(orderedIDs []string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, id := range orderedIDs {
		order := (i + 1) * 10
		if _, err := tx.Exec("UPDATE plugins SET pipeline_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", order, id); err != nil {
			return fmt.Errorf("failed to update order for %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	_ = m.ReloadCache()
	return nil
}

// ListBindings returns all bindings for a plugin.
func (m *Manager) ListBindings(pluginID string) ([]Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Binding
	for _, b := range m.bindings {
		if b.PluginID == pluginID {
			result = append(result, b)
		}
	}
	return result, nil
}

// UpsertBinding saves or updates a binding. When state is inherit for non-global scopes,
// it removes the binding record to maintain sparse overrides.
func (m *Manager) UpsertBinding(b Binding) error {
	state := b.State
	if state == "" {
		state = StateInherit
	}
	settings := strings.TrimSpace(b.Settings)
	if settings == "" {
		settings = "{}"
	}

	if b.ScopeType != ScopeGlobal && state == StateInherit {
		_, err := m.db.Exec(`
			DELETE FROM plugin_bindings WHERE plugin_id = ? AND scope_type = ? AND scope_id = ?
		`, b.PluginID, b.ScopeType, b.ScopeID)
		if err != nil {
			return fmt.Errorf("delete inherit binding error: %w", err)
		}
		_ = m.ReloadCache()
		return nil
	}

	_, err := m.db.Exec(`
		INSERT INTO plugin_bindings (plugin_id, scope_type, scope_id, state, settings, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(plugin_id, scope_type, scope_id) DO UPDATE SET
			state = excluded.state,
			settings = excluded.settings,
			updated_at = CURRENT_TIMESTAMP
	`, b.PluginID, b.ScopeType, b.ScopeID, state, settings)
	if err != nil {
		return fmt.Errorf("upsert binding error: %w", err)
	}

	_ = m.ReloadCache()
	return nil
}

// ListBindingsForScope returns all active non-inherit bindings for a specific scope (key or group).
func (m *Manager) ListBindingsForScope(scopeType ScopeType, scopeID string) ([]Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Binding
	for _, b := range m.bindings {
		if b.ScopeType == scopeType && b.ScopeID == scopeID {
			result = append(result, b)
		}
	}
	return result, nil
}

// ResetPromptOverride clears prompt_override from the plugin settings.
func (m *Manager) ResetPromptOverride(pluginID string) error {
	p, err := m.GetPluginRaw(pluginID)
	if err != nil {
		return err
	}

	var mSettings map[string]any
	_ = json.Unmarshal([]byte(p.DefaultSettings), &mSettings)
	if mSettings != nil {
		delete(mSettings, "prompt_override")
		cleaned, _ := json.Marshal(mSettings)
		_, _ = m.db.Exec("UPDATE plugins SET default_settings = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", string(cleaned), pluginID)
	}

	// Also clear from global binding if overridden there
	var globSettings string
	err = m.db.QueryRow("SELECT settings FROM plugin_bindings WHERE plugin_id = ? AND scope_type = 'global'", pluginID).Scan(&globSettings)
	if err == nil {
		var gMap map[string]any
		_ = json.Unmarshal([]byte(globSettings), &gMap)
		if gMap != nil && gMap["prompt_override"] != nil {
			delete(gMap, "prompt_override")
			cleaned, _ := json.Marshal(gMap)
			_, _ = m.db.Exec("UPDATE plugin_bindings SET settings = ?, updated_at = CURRENT_TIMESTAMP WHERE plugin_id = ? AND scope_type = 'global'", string(cleaned), pluginID)
		}
	}

	_ = m.ReloadCache()
	return nil
}

// ExecutePipeline runs plugins against the request body.
func (m *Manager) ExecutePipeline(
	ctx context.Context,
	groups []models.ModelGroup,
	apiKeyID, apiKeyName string,
	model, providerID string,
	isBypassRequested bool,
	incomingAppliedHeader string,
	bodyBytes []byte,
) (*PipelineResult, string, error) {
	m.mu.RLock()
	pluginsCopy := make([]Plugin, len(m.plugins))
	copy(pluginsCopy, m.plugins)
	bindingsCopy := make([]Binding, len(m.bindings))
	copy(bindingsCopy, m.bindings)
	m.mu.RUnlock()

	return m.executor.Execute(
		ctx,
		pluginsCopy,
		bindingsCopy,
		groups,
		apiKeyID,
		apiKeyName,
		model,
		providerID,
		isBypassRequested,
		incomingAppliedHeader,
		bodyBytes,
	)
}

// ResolveEffective returns effective plugins and scope bindings for a (key, model) pair.
func (m *Manager) ResolveEffective(
	groups []models.ModelGroup,
	apiKeyID, apiKeyName string,
	model string,
) []ResolvedPlugin {
	m.mu.RLock()
	pluginsCopy := make([]Plugin, len(m.plugins))
	copy(pluginsCopy, m.plugins)
	bindingsCopy := make([]Binding, len(m.bindings))
	copy(bindingsCopy, m.bindings)
	m.mu.RUnlock()

	return ResolvePluginsForRequest(pluginsCopy, bindingsCopy, groups, apiKeyID, apiKeyName, model)
}
