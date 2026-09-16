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
)

type Provider struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Route     string    `json:"route"`
	APIKey    string    `json:"api_key"`
	MaskedKey string    `json:"masked_key"`
	Prefix    string    `json:"prefix"`
	IsDefault bool      `json:"is_default"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Manager struct {
	db *db.DB
	mu sync.RWMutex
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
		return "sk-..." + key[len(key)-2:]
	}
	return key[:6] + "..." + key[len(key)-4:]
}

func (m *Manager) ListProviders() ([]Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query(`
		SELECT id, name, route, COALESCE(api_key, ''), prefix, is_default, is_active, created_at, updated_at
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
		var defInt, actInt int
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Route, &p.APIKey, &p.Prefix,
			&defInt, &actInt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.IsDefault = (defInt == 1)
		p.IsActive = (actInt == 1)
		p.MaskedKey = maskKey(p.APIKey)
		list = append(list, p)
	}

	return list, nil
}

func (m *Manager) GetProvider(id string) (*Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var p Provider
	var defInt, actInt int
	err := m.db.QueryRow(`
		SELECT id, name, route, COALESCE(api_key, ''), prefix, is_default, is_active, created_at, updated_at
		FROM providers WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Route, &p.APIKey, &p.Prefix, &defInt, &actInt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.IsDefault = (defInt == 1)
	p.IsActive = (actInt == 1)
	p.MaskedKey = maskKey(p.APIKey)
	return &p, nil
}

func (m *Manager) CreateProvider(name, route, apiKey, prefix string, isDefault, isActive bool) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimSpace(name)
	route = strings.TrimRight(strings.TrimSpace(route), "/")
	apiKey = strings.TrimSpace(apiKey)
	prefix = strings.ToLower(strings.Trim(strings.TrimSpace(prefix), "/"))

	if name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if route == "" {
		return nil, fmt.Errorf("route endpoint URL is required")
	}

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

	defInt := 0
	if isDefault {
		defInt = 1
		// Only one default provider
		_, _ = m.db.Exec("UPDATE providers SET is_default = 0")
	}

	actInt := 0
	if isActive {
		actInt = 1
	}

	now := time.Now()
	_, err := m.db.Exec(`
		INSERT INTO providers (id, name, route, api_key, prefix, is_default, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, route, apiKey, prefix, defInt, actInt, now, now)
	if err != nil {
		return nil, err
	}

	return &Provider{
		ID:        id,
		Name:      name,
		Route:     route,
		APIKey:    apiKey,
		MaskedKey: maskKey(apiKey),
		Prefix:    prefix,
		IsDefault: isDefault,
		IsActive:  isActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (m *Manager) UpdateProvider(id, name, route, apiKey, prefix string, isDefault, isActive bool) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimSpace(name)
	route = strings.TrimRight(strings.TrimSpace(route), "/")
	apiKey = strings.TrimSpace(apiKey)
	prefix = strings.ToLower(strings.Trim(strings.TrimSpace(prefix), "/"))

	if name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if route == "" {
		return nil, fmt.Errorf("route endpoint URL is required")
	}

	defInt := 0
	if isDefault {
		defInt = 1
		_, _ = m.db.Exec("UPDATE providers SET is_default = 0 WHERE id != ?", id)
	}

	actInt := 0
	if isActive {
		actInt = 1
	}

	_, err := m.db.Exec(`
		UPDATE providers SET name = ?, route = ?, api_key = ?, prefix = ?, is_default = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, route, apiKey, prefix, defInt, actInt, id)
	if err != nil {
		return nil, err
	}

	return m.GetProvider(id)
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

	// 1. Get provider info (prefix) before deleting
	var prefix string
	_ = m.db.QueryRow("SELECT prefix FROM providers WHERE id = ?", id).Scan(&prefix)

	// 2. Delete provider from providers table
	_, err := m.db.Exec("DELETE FROM providers WHERE id = ?", id)
	if err != nil {
		return err
	}

	// 3. Automatically delete all models belonging to this provider
	if prefix != "" {
		_, _ = m.db.Exec("DELETE FROM models WHERE provider_id = ? OR provider_id = ? OR id LIKE ?", id, prefix, prefix+"/%")
	} else {
		_, _ = m.db.Exec("DELETE FROM models WHERE provider_id = ?", id)
	}

	return nil
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
			reqURL := prov.Route
			if !strings.HasSuffix(reqURL, "/v1") {
				reqURL += "/v1"
			}
			reqURL += "/models"

			req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
			if err != nil {
				return
			}
			if prov.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+prov.APIKey)
			}

			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				// Try fallback without /v1
				fallbackURL := prov.Route + "/models"
				req2, err2 := http.NewRequestWithContext(ctx, "GET", fallbackURL, nil)
				if err2 == nil {
					if prov.APIKey != "" {
						req2.Header.Set("Authorization", "Bearer "+prov.APIKey)
					}
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
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

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
