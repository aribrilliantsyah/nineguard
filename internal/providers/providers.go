package providers

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
	"nineguard/internal/vault"
)

type Provider struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	Route                   string    `json:"route"`
	APIKey                  string    `json:"-"`
	MaskedKey               string    `json:"masked_key"`
	Prefix                  string    `json:"prefix"`
	IsDefault               bool      `json:"is_default"`
	IsActive                bool      `json:"is_active"`
	UpstreamTokenSaving     bool      `json:"upstream_token_saving"`
	UpstreamTokenSavingNote string    `json:"upstream_token_saving_note"`
	QuotaType               string    `json:"quota_type"`
	QuotaAuthSecret         string    `json:"-"`
	HasQuotaAuthSecret      bool      `json:"has_quota_auth_secret"`
	Icon                    string    `json:"icon"`
	Backend                 string    `json:"backend"`
	Family                  string    `json:"family"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type ProviderOptions struct {
	Name                    string
	Route                   string
	APIKey                  string
	Prefix                  string
	IsDefault               bool
	IsActive                bool
	UpstreamTokenSaving     bool
	UpstreamTokenSavingNote string
	QuotaType               string
	QuotaAuthSecret         *string // nil keeps existing on update
	Icon                    *string // nil keeps existing on update; "" = auto
	Backend                 *string // nil preserves existing backend on update
	Family                  *string // nil preserves existing family on update
}

type EngineCatalog interface {
	Models(context.Context, string) ([]map[string]interface{}, error)
	RemoveProvider(context.Context, string) error
}

type Manager struct {
	db *db.DB
	mu sync.RWMutex
	engine EngineCatalog
}

func (m *Manager) SetEngine(engine EngineCatalog) { m.engine = engine }

// SDKModelIDs returns only this SDK provider's public catalog for quota visibility checks.
func (m *Manager) SDKModelIDs(ctx context.Context, p Provider) []string {
	if p.Backend != "cliproxy" || m.engine == nil { return nil }
	items, err := m.engine.Models(ctx, p.ID)
	if err != nil { return nil }
	ids := make([]string, 0, len(items))
	for _, item := range items { if id, ok := item["id"].(string); ok { if p.Prefix != "" { id = p.Prefix + "/" + id }; ids = append(ids, id) } }
	return ids
}

func normalizeQuotaType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "9router":
		return "9router"
	case "nineguard":
		return "nineguard"
	default:
		return "disabled"
	}
}

func NewManager(database *db.DB) *Manager {
	return &Manager{db: database}
}

func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(strings.ToLower(key), "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	if len(key) <= 10 {
		if len(key) == 0 {
			return "none"
		}
		// Showing the last 2 chars of a very short key reveals all or most of it (and slicing a
		// 1-char key panics), so keys of 4 chars or fewer show no tail.
		if len(key) <= 4 {
			return "sk-..."
		}
		return "sk-..." + key[len(key)-2:]
	}
	return key[:6] + "..." + key[len(key)-4:]
}

func (m *Manager) ListProviders() ([]Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`
		SELECT id, name, route, COALESCE(api_key, ''), prefix, is_default, is_active,
		       COALESCE(upstream_token_saving, 0), COALESCE(upstream_token_saving_note, ''),
		       COALESCE(quota_type, 'disabled'), COALESCE(quota_auth_secret, ''),
		       COALESCE(icon, ''), COALESCE(backend, 'http'), COALESCE(family, ''), created_at, updated_at
		FROM providers
		ORDER BY is_default DESC, is_active DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Provider
	for rows.Next() {
		var p Provider
		var defInt, actInt, utsInt int
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Route, &p.APIKey, &p.Prefix,
			&defInt, &actInt, &utsInt, &p.UpstreamTokenSavingNote,
			&p.QuotaType, &p.QuotaAuthSecret, &p.Icon, &p.Backend, &p.Family,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.IsDefault = (defInt == 1)
		p.IsActive = (actInt == 1)
		p.UpstreamTokenSaving = (utsInt == 1)
		p.QuotaType = normalizeQuotaType(p.QuotaType)
		p.APIKey, _ = vault.Decrypt(p.APIKey)
		p.QuotaAuthSecret, _ = vault.Decrypt(p.QuotaAuthSecret)
		p.HasQuotaAuthSecret = strings.TrimSpace(p.QuotaAuthSecret) != ""
		p.MaskedKey = maskKey(p.APIKey)
		list = append(list, p)
	}

	return list, nil
}

func (m *Manager) GetProvider(id string) (*Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getProviderUnlocked(id)
}

func (m *Manager) getProviderUnlocked(id string) (*Provider, error) {
	var p Provider
	var defInt, actInt, utsInt int
	err := m.db.QueryRow(`
		SELECT id, name, route, COALESCE(api_key, ''), prefix, is_default, is_active,
		       COALESCE(upstream_token_saving, 0), COALESCE(upstream_token_saving_note, ''),
		       COALESCE(quota_type, 'disabled'), COALESCE(quota_auth_secret, ''),
		       COALESCE(icon, ''), COALESCE(backend, 'http'), COALESCE(family, ''), created_at, updated_at
		FROM providers WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Route, &p.APIKey, &p.Prefix, &defInt, &actInt, &utsInt, &p.UpstreamTokenSavingNote, &p.QuotaType, &p.QuotaAuthSecret, &p.Icon, &p.Backend, &p.Family, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.IsDefault = (defInt == 1)
	p.IsActive = (actInt == 1)
	p.UpstreamTokenSaving = (utsInt == 1)
	p.QuotaType = normalizeQuotaType(p.QuotaType)
	p.APIKey, _ = vault.Decrypt(p.APIKey)
	p.QuotaAuthSecret, _ = vault.Decrypt(p.QuotaAuthSecret)
	p.HasQuotaAuthSecret = strings.TrimSpace(p.QuotaAuthSecret) != ""
	p.MaskedKey = maskKey(p.APIKey)
	return &p, nil
}

func (m *Manager) CreateProvider(name, route, apiKey, prefix string, isDefault, isActive bool) (*Provider, error) {
	return m.CreateProviderWithOptions(ProviderOptions{
		Name:      name,
		Route:     route,
		APIKey:    apiKey,
		Prefix:    prefix,
		IsDefault: isDefault,
		IsActive:  isActive,
	})
}

func (m *Manager) CreateProviderWithTokenSaving(name, route, apiKey, prefix string, isDefault, isActive, upstreamTokenSaving bool, upstreamTokenSavingNote string) (*Provider, error) {
	return m.CreateProviderWithOptions(ProviderOptions{
		Name:                    name,
		Route:                   route,
		APIKey:                  apiKey,
		Prefix:                  prefix,
		IsDefault:               isDefault,
		IsActive:                isActive,
		UpstreamTokenSaving:     upstreamTokenSaving,
		UpstreamTokenSavingNote: upstreamTokenSavingNote,
	})
}

func (m *Manager) CreateProviderWithOptions(opts ProviderOptions) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := strings.TrimSpace(opts.Name)
	route := strings.TrimRight(strings.TrimSpace(opts.Route), "/")
	apiKey := strings.TrimSpace(opts.APIKey)
	prefix := strings.ToLower(strings.Trim(strings.TrimSpace(opts.Prefix), "/"))
	upstreamTokenSavingNote := strings.TrimSpace(opts.UpstreamTokenSavingNote)
	if len(upstreamTokenSavingNote) > 200 {
		upstreamTokenSavingNote = upstreamTokenSavingNote[:200]
	}
	quotaType := normalizeQuotaType(opts.QuotaType)
	quotaSecret := ""
	if opts.QuotaAuthSecret != nil {
		quotaSecret = strings.TrimSpace(*opts.QuotaAuthSecret)
	}
	icon := ""
	if opts.Icon != nil {
		var err error
		if icon, err = normalizeIcon(*opts.Icon); err != nil {
			return nil, err
		}
	}

	if name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	backend, family := "http", ""
	if opts.Backend != nil { backend = *opts.Backend }
	if opts.Family != nil { family = *opts.Family }
	backend, family, identityErr := normalizeBackend(backend, family, route, apiKey)
	if identityErr != nil { return nil, identityErr }

	idBytes := make([]byte, 8)
	_, _ = rand.Read(idBytes)
	id := prefix
	if id == "" {
		id = hex.EncodeToString(idBytes)
	} else {
		// Ensure unique ID
		var exists int
		_ = m.db.QueryRow("SELECT COUNT(*) FROM providers WHERE id = ?", id).Scan(&exists)
		if exists > 0 {
			id = id + "-" + hex.EncodeToString(idBytes[:4])
		}
	}

	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	defInt := 0
	isDefault := opts.IsDefault
	if isDefault {
		defInt = 1
		// When newly set as default, clear default flag from all other providers
		if _, err := tx.Exec("UPDATE providers SET is_default = 0"); err != nil {
			return nil, fmt.Errorf("failed to clear existing default providers: %w", err)
		}
	} else {
		// If not explicitly set as default, check if any default already exists.
		// If none exists (e.g. first provider ever), make this one default.
		var existingDefaultCount int
		_ = tx.QueryRow("SELECT COUNT(*) FROM providers WHERE is_default = 1").Scan(&existingDefaultCount)
		if existingDefaultCount == 0 {
			defInt = 1
			isDefault = true
		}
	}

	actInt := 0
	if opts.IsActive {
		actInt = 1
	}

	utsInt := 0
	if opts.UpstreamTokenSaving {
		utsInt = 1
	}

	now := time.Now()
	encAPIKey, _ := vault.Encrypt(apiKey)
	encQuotaSecret, _ := vault.Encrypt(quotaSecret)
	_, err = tx.Exec(`
		INSERT INTO providers (id, name, route, api_key, prefix, is_default, is_active, upstream_token_saving, upstream_token_saving_note, quota_type, quota_auth_secret, icon, backend, family, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, route, encAPIKey, prefix, defInt, actInt, utsInt, upstreamTokenSavingNote, quotaType, encQuotaSecret, icon, backend, family, now, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Provider{
		ID:                      id,
		Name:                    name,
		Route:                   route,
		APIKey:                  apiKey,
		MaskedKey:               maskKey(apiKey),
		Prefix:                  prefix,
		IsDefault:               isDefault,
		IsActive:                opts.IsActive,
		UpstreamTokenSaving:     opts.UpstreamTokenSaving,
		UpstreamTokenSavingNote: upstreamTokenSavingNote,
		QuotaType:               quotaType,
		QuotaAuthSecret:         quotaSecret,
		HasQuotaAuthSecret:      quotaSecret != "",
		Icon:                    icon,
		Backend:                 backend,
		Family:                  family,
		CreatedAt:               now,
		UpdatedAt:               now,
	}, nil
}

func (m *Manager) UpdateProvider(id, name, route, apiKey, prefix string, isDefault, isActive bool) (*Provider, error) {
	curr, err := m.GetProvider(id)
	uts := false
	utsNote := ""
	if err == nil && curr != nil {
		uts = curr.UpstreamTokenSaving
		utsNote = curr.UpstreamTokenSavingNote
	}
	return m.UpdateProviderWithTokenSaving(id, name, route, apiKey, prefix, isDefault, isActive, uts, utsNote)
}

func (m *Manager) UpdateProviderWithTokenSaving(id, name, route, apiKey, prefix string, isDefault, isActive, upstreamTokenSaving bool, upstreamTokenSavingNote string) (*Provider, error) {
	return m.UpdateProviderWithOptions(id, ProviderOptions{
		Name:                    name,
		Route:                   route,
		APIKey:                  apiKey,
		Prefix:                  prefix,
		IsDefault:               isDefault,
		IsActive:                isActive,
		UpstreamTokenSaving:     upstreamTokenSaving,
		UpstreamTokenSavingNote: upstreamTokenSavingNote,
	})
}

func (m *Manager) UpdateProviderWithOptions(id string, opts ProviderOptions) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	curr, err := m.getProviderUnlocked(id)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	name := strings.TrimSpace(opts.Name)
	route := strings.TrimRight(strings.TrimSpace(opts.Route), "/")
	apiKey := strings.TrimSpace(opts.APIKey)
	prefix := strings.ToLower(strings.Trim(strings.TrimSpace(opts.Prefix), "/"))
	upstreamTokenSavingNote := strings.TrimSpace(opts.UpstreamTokenSavingNote)
	if len(upstreamTokenSavingNote) > 200 {
		upstreamTokenSavingNote = upstreamTokenSavingNote[:200]
	}

	quotaType := curr.QuotaType
	if opts.QuotaType != "" {
		quotaType = normalizeQuotaType(opts.QuotaType)
	}
	quotaSecret := curr.QuotaAuthSecret
	if opts.QuotaAuthSecret != nil {
		quotaSecret = strings.TrimSpace(*opts.QuotaAuthSecret)
	}
	icon := curr.Icon
	if opts.Icon != nil {
		if icon, err = normalizeIcon(*opts.Icon); err != nil {
			return nil, err
		}
	}

	if name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	backend, family := curr.Backend, curr.Family
	if opts.Backend != nil { backend = *opts.Backend }
	if opts.Family != nil { family = *opts.Family }
	backend, family, err = normalizeBackend(backend, family, route, apiKey)
	if err != nil { return nil, err }
	if curr.Backend != backend || curr.Family != family {
		return nil, fmt.Errorf("provider connection type cannot be changed; create a separate provider")
	}

	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	defInt := 0
	isDefault := opts.IsDefault
	if isDefault {
		defInt = 1
		// When set as default, reset all other providers so exactly this one is default
		if _, err := tx.Exec("UPDATE providers SET is_default = 0 WHERE id != ?", id); err != nil {
			return nil, fmt.Errorf("failed to clear existing default providers: %w", err)
		}
	} else {
		// Check if another default provider exists
		var otherDefaultCount int
		_ = tx.QueryRow("SELECT COUNT(*) FROM providers WHERE is_default = 1 AND id != ?", id).Scan(&otherDefaultCount)
		if otherDefaultCount == 0 {
			// Ensure there is always at least one default provider
			defInt = 1
			isDefault = true
		}
	}

	actInt := 0
	if opts.IsActive {
		actInt = 1
	}

	utsInt := 0
	if opts.UpstreamTokenSaving {
		utsInt = 1
	}

	encAPIKey, _ := vault.Encrypt(apiKey)
	encQuotaSecret, _ := vault.Encrypt(quotaSecret)
	_, err = tx.Exec(`
		UPDATE providers SET name = ?, route = ?, api_key = ?, prefix = ?, is_default = ?, is_active = ?, upstream_token_saving = ?, upstream_token_saving_note = ?, quota_type = ?, quota_auth_secret = ?, icon = ?, backend = ?, family = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, route, encAPIKey, prefix, defInt, actInt, utsInt, upstreamTokenSavingNote, quotaType, encQuotaSecret, icon, backend, family, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return m.getProviderUnlocked(id)
}

func (m *Manager) SetDefaultProvider(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var exists int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM providers WHERE id = ?", id).Scan(&exists); err != nil || exists == 0 {
		return fmt.Errorf("provider not found")
	}

	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Atomically ensure only this provider is default and all others are not default
	if _, err := tx.Exec("UPDATE providers SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END, updated_at = CURRENT_TIMESTAMP", id); err != nil {
		return err
	}

	return tx.Commit()
}

func (m *Manager) ToggleProvider(id string, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	actInt := 0
	if active {
		actInt = 1
	}
	_, err := m.db.Exec("UPDATE providers SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", actInt, id)
	return err
}

func (m *Manager) DeleteProvider(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.engine != nil { if err := m.engine.RemoveProvider(context.Background(), id); err != nil { return err } }
	// 1. Get provider info (prefix, is_default) before deleting
	var prefix string
	var isDef int
	_ = m.db.QueryRow("SELECT prefix, is_default FROM providers WHERE id = ?", id).Scan(&prefix, &isDef)

	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 2. Delete provider from providers table
	if _, err := tx.Exec("DELETE FROM providers WHERE id = ?", id); err != nil {
		return err
	}

	// 3. Automatically delete all models belonging to this provider
	if prefix != "" {
		_, _ = tx.Exec("DELETE FROM models WHERE provider_id = ? OR provider_id = ? OR id LIKE ?", id, prefix, prefix+"/%")
	} else {
		_, _ = tx.Exec("DELETE FROM models WHERE provider_id = ?", id)
	}

	// 4. If the deleted provider was default, promote another active provider as default
	if isDef == 1 {
		_, _ = tx.Exec(`
			UPDATE providers SET is_default = 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = (SELECT id FROM providers WHERE is_active = 1 ORDER BY created_at ASC LIMIT 1)
		`)
	}

	return tx.Commit()
}

// FindProviderForModel routes a requested model to the right upstream provider.
// If model is "openrouter/anthropic/claude-3.5-sonnet" and provider "openrouter" has prefix "openrouter",
// it strips the prefix and returns the provider with actual model "anthropic/claude-3.5-sonnet".
func (m *Manager) FindProviderForModel(modelName string) (*Provider, string, error) {
	providers, err := m.ListProviders()
	if err != nil {
		return nil, "", err
	}

	modelName = strings.TrimSpace(modelName)

	var activeProviders []Provider
	var defaultProvider *Provider

	for _, p := range providers {
		if p.IsActive {
			activeProviders = append(activeProviders, p)
			if p.IsDefault {
				pCopy := p
				defaultProvider = &pCopy
			}
		}
	}

	if len(activeProviders) == 0 {
		return nil, "", fmt.Errorf("no active upstream providers configured in NineGuard")
	}

	// 1. Check if model starts with any provider's prefix + "/"
	for _, p := range activeProviders {
		if p.Prefix != "" {
			prefixPattern := p.Prefix + "/"
			if strings.HasPrefix(modelName, prefixPattern) {
				actualModel := strings.TrimPrefix(modelName, prefixPattern)
				pCopy := p
				return &pCopy, actualModel, nil
			}
		}
	}

	// 2. Check fallback default provider
	if defaultProvider != nil {
		return defaultProvider, modelName, nil
	}

	// 3. Fallback to the first active provider
	first := activeProviders[0]
	return &first, modelName, nil
}

// AggregateModels queries all active upstream providers and returns a merged list of OpenAI models.
// If a provider has a prefix like "openrouter", models are prefixed with "openrouter/model-id" for clear grouping.
func (m *Manager) AggregateModels(ctx context.Context) ([]map[string]interface{}, error) {
	providers, err := m.ListProviders()
	if err != nil {
		return nil, err
	}

	type modelResult struct {
		providerID string
		prefix     string
		models     []map[string]interface{}
	}

	var active []Provider
	for _, p := range providers {
		if p.IsActive {
			active = append(active, p)
		}
	}

	if len(active) == 0 {
		return []map[string]interface{}{}, nil
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resultsChan := make(chan modelResult, len(active))

	var wg sync.WaitGroup
	for _, p := range active {
		wg.Add(1)
		go func(prov Provider) {
			defer wg.Done()
			if prov.Backend == "cliproxy" {
				if m.engine != nil {
					if items, err := m.engine.Models(ctx, prov.ID); err == nil { resultsChan <- modelResult{providerID: prov.ID, prefix: prov.Prefix, models: items} }
				}
				return
			}
			reqURL := prov.Route
			if !strings.HasSuffix(reqURL, "/v1") {
				reqURL += "/v1"
			}
			reqURL += "/models"

			req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
			if err != nil {
				return
			}
			setUpstreamAuth(req, prov.Route, prov.APIKey)

			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				// Try fallback without /v1
				fallbackURL := prov.Route + "/models"
				req2, err2 := http.NewRequestWithContext(ctx, "GET", fallbackURL, nil)
				if err2 == nil {
					setUpstreamAuth(req2, prov.Route, prov.APIKey)
					resp2, err3 := client.Do(req2)
					if err3 == nil && resp2.StatusCode == http.StatusOK {
						resp = resp2
					} else {
						return
					}
				} else {
					return
				}
			}
			defer resp.Body.Close()

			var payload struct {
				Data []map[string]interface{} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				resultsChan <- modelResult{
					providerID: prov.ID,
					prefix:     prov.Prefix,
					models:     payload.Data,
				}
			}
		}(p)
	}

	wg.Wait()
	close(resultsChan)

	var combined []map[string]interface{}
	seenIDs := make(map[string]bool)

	for res := range resultsChan {
		for _, mItem := range res.models {
			rawID, ok := mItem["id"].(string)
			if !ok || rawID == "" {
				continue
			}

			finalID := rawID
			if res.prefix != "" && !strings.HasPrefix(rawID, res.prefix+"/") {
				finalID = res.prefix + "/" + rawID
			}

			if seenIDs[finalID] {
				continue
			}
			seenIDs[finalID] = true

			mCopy := make(map[string]interface{})
			for k, v := range mItem {
				mCopy[k] = v
			}
			mCopy["id"] = finalID
			mCopy["provider"] = res.providerID
			combined = append(combined, mCopy)
		}
	}

	return combined, nil
}

// TestProvider sends an OpenAI-compatible test probe to the specified route and API key.
func (m *Manager) TestProvider(ctx context.Context, route, apiKey string) (map[string]interface{}, error) {
	route = strings.TrimRight(strings.TrimSpace(route), "/")
	apiKey = strings.TrimSpace(apiKey)

	if route == "" {
		return nil, fmt.Errorf("route endpoint URL is required")
	}

	start := time.Now()
	client := &http.Client{Timeout: 8 * time.Second}

	reqURL := route
	if !strings.HasSuffix(reqURL, "/v1") {
		reqURL += "/v1"
	}
	reqURL += "/models"

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	setUpstreamAuth(req, route, apiKey)

	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())

	if err != nil {
		return map[string]interface{}{
			"ok":         false,
			"error":      fmt.Sprintf("Cannot reach provider at %s: %v", route, err),
			"latency_ms": latency,
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return map[string]interface{}{
			"ok":         false,
			"status":     401,
			"error":      "Provider returned HTTP 401 Unauthorized (Invalid API Key)",
			"latency_ms": latency,
		}, nil
	}

	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)

	return map[string]interface{}{
		"ok":           true,
		"status":       resp.StatusCode,
		"models_count": len(payload.Data),
		"latency_ms":   latency,
		"target":       route,
	}, nil
}

// ResolveModelID routes a requested model (same rules as FindProviderForModel) and
// returns the canonical Model ID: "<prefix>/<actual>" when the provider has a prefix,
// otherwise "<actual>". An empty request yields an empty id (ADR 0008).
func (m *Manager) ResolveModelID(requested string) (*Provider, string, string, error) {
	p, actual, err := m.FindProviderForModel(requested)
	if err != nil {
		return nil, "", "", err
	}
	if strings.TrimSpace(requested) == "" {
		return p, actual, "", nil
	}
	if p.Prefix != "" {
		return p, actual, p.Prefix + "/" + actual, nil
	}
	return p, actual, actual, nil
}

// IsCanonicalModelID is true when id resolves to itself (case-insensitive).
func (m *Manager) IsCanonicalModelID(id string) bool {
	_, _, resolved, err := m.ResolveModelID(id)
	return err == nil && resolved != "" && strings.EqualFold(resolved, strings.TrimSpace(id))
}
