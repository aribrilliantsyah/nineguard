package keys

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
)

type KeyInfo struct {
	ID            string    `json:"id"`
	Key           string    `json:"key"` // masked key for display
	RawKey        string    `json:"raw_key,omitempty"`
	Prefix        string    `json:"prefix"`
	Name          string    `json:"name"`
	IsActive      bool      `json:"is_active"`
	TotalRequests int       `json:"total_requests"`
	TotalTokens   int       `json:"total_tokens"`
	LastUsedAt    *string   `json:"last_used_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Manager struct {
	db          *db.DB
	mu          sync.RWMutex
	upstreamKey string
	cache       map[string]*KeyInfo // rawKey -> KeyInfo
}

func NewManager(database *db.DB, defaultRouterAPIKey string) *Manager {
	m := &Manager{
		db:          database,
		upstreamKey: strings.TrimSpace(defaultRouterAPIKey),
		cache:       make(map[string]*KeyInfo),
	}

	// 1. Load Upstream 9router key from database settings (overrides env if set in UI)
	var dbUpstream string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'router_api_key' LIMIT 1").Scan(&dbUpstream)
	if err == nil && strings.TrimSpace(dbUpstream) != "" {
		m.upstreamKey = strings.TrimSpace(dbUpstream)
	}

	// 2. Load NineGuard API keys from database into cache
	m.loadLocalCache()

	// 3. Ensure at least one default NineGuard API key exists if database is empty
	m.ensureDefaultKey()

	return m
}

func MaskKey(key string) string {
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

func generateSecureToken(prefix string) (string, string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(bytes)
	fullKey := prefix + token
	keyPrefix := fullKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}
	return fullKey, keyPrefix, nil
}

// ── Upstream 9router Key Methods ──

func (m *Manager) GetUpstreamTarget(defaultTarget string) string {
	var target string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'router_target' LIMIT 1").Scan(&target)
	if err == nil && strings.TrimSpace(target) != "" {
		return strings.TrimRight(strings.TrimSpace(target), "/")
	}
	return strings.TrimRight(strings.TrimSpace(defaultTarget), "/")
}

func (m *Manager) SetUpstreamTarget(target string) error {
	target = strings.TrimRight(strings.TrimSpace(target), "/")
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('router_target', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, target)
	return err
}

func (m *Manager) GetUpstreamKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.upstreamKey
}

func (m *Manager) SetUpstreamKey(key string) error {
	key = strings.TrimSpace(key)
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('router_api_key', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, key)
	if err == nil {
		m.mu.Lock()
		m.upstreamKey = key
		m.mu.Unlock()
	}
	return err
}

func (m *Manager) GetUpstreamInfo() map[string]interface{} {
	m.mu.RLock()
	k := m.upstreamKey
	m.mu.RUnlock()

	configured := k != ""
	masked := ""
	if configured {
		masked = MaskKey(k)
	}
	return map[string]interface{}{
		"configured": configured,
		"masked_key": masked,
		"key":        k,
	}
}

// ── NineGuard Client API Keys Management ──

func (m *Manager) loadLocalCache() {
	rows, err := m.db.Query("SELECT id, key, prefix, name, is_active FROM api_keys")
	if err != nil {
		return
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	for rows.Next() {
		var ki KeyInfo
		var isAct int
		if err := rows.Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct); err == nil {
			ki.IsActive = (isAct == 1)
			ki.Key = MaskKey(ki.RawKey)
			m.cache[ki.RawKey] = &ki
		}
	}
}

func (m *Manager) ensureDefaultKey() {
	var count int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&count)
	if count == 0 {
		_, _ = m.CreateKey("Default Agent Key")
		slog.Info("created initial default NineGuard API key")
	}
}

// CreateKey issues a new NineGuard API key
func (m *Manager) CreateKey(name string) (*KeyInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Agent Key"
	}

	rawKey, prefix, err := generateSecureToken("sk-ng-")
	if err != nil {
		return nil, fmt.Errorf("failed to generate key token: %w", err)
	}

	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)

	now := time.Now()
	_, err = m.db.Exec(`
		INSERT INTO api_keys (id, key, prefix, name, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, rawKey, prefix, name)
	if err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}

	ki := &KeyInfo{
		ID:            id,
		Key:           MaskKey(rawKey),
		RawKey:        rawKey,
		Prefix:        prefix,
		Name:          name,
		IsActive:      true,
		TotalRequests: 0,
		TotalTokens:   0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	m.mu.Lock()
	m.cache[rawKey] = ki
	m.mu.Unlock()

	return ki, nil
}

// ListKeys returns all NineGuard API keys with request and token stats
func (m *Manager) ListKeys() ([]KeyInfo, error) {
	query := `
		SELECT 
			k.id,
			k.key,
			k.prefix,
			k.name,
			k.is_active,
			k.created_at,
			k.updated_at,
			COUNT(t.id) as total_requests,
			COALESCE(SUM(t.total_tokens), 0) as total_tokens,
			MAX(t.timestamp) as last_used_at
		FROM api_keys k
		LEFT JOIN traffic_logs t ON (t.api_key = k.key OR t.api_key_name = k.name OR t.api_key LIKE '%' || k.prefix || '%')
		GROUP BY k.id
		ORDER BY k.created_at DESC
	`
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []KeyInfo
	for rows.Next() {
		var ki KeyInfo
		var rawKey string
		var isActiveInt int
		var lastUsed sql.NullString
		if err := rows.Scan(
			&ki.ID,
			&rawKey,
			&ki.Prefix,
			&ki.Name,
			&isActiveInt,
			&ki.CreatedAt,
			&ki.UpdatedAt,
			&ki.TotalRequests,
			&ki.TotalTokens,
			&lastUsed,
		); err != nil {
			return nil, err
		}
		ki.IsActive = (isActiveInt == 1)
		ki.Key = MaskKey(rawKey)
		ki.RawKey = rawKey
		if lastUsed.Valid {
			ki.LastUsedAt = &lastUsed.String
		}
		list = append(list, ki)
	}

	return list, nil
}

// ToggleKey activates or deactivates an API key
func (m *Manager) ToggleKey(id string, active bool) error {
	actInt := 0
	if active {
		actInt = 1
	}
	_, err := m.db.Exec("UPDATE api_keys SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", actInt, id)
	if err != nil {
		return err
	}

	// Refresh cache
	m.loadLocalCache()
	return nil
}

// DeleteKey removes an API key
func (m *Manager) DeleteKey(id string) error {
	var rawKey string
	_ = m.db.QueryRow("SELECT key FROM api_keys WHERE id = ?", id).Scan(&rawKey)

	_, err := m.db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return err
	}

	if rawKey != "" {
		m.mu.Lock()
		delete(m.cache, rawKey)
		m.mu.Unlock()
	}
	return nil
}

// ValidateClientKey checks if an incoming client Bearer token is a valid, active NineGuard key
func (m *Manager) ValidateClientKey(rawKey string) (*KeyInfo, bool) {
	rawKey = strings.TrimSpace(rawKey)
	if strings.HasPrefix(strings.ToLower(rawKey), "bearer ") {
		rawKey = strings.TrimSpace(rawKey[7:])
	}
	if rawKey == "" {
		return nil, false
	}

	m.mu.RLock()
	cached, found := m.cache[rawKey]
	m.mu.RUnlock()

	if found && cached != nil {
		if cached.IsActive {
			return cached, true
		}
		return nil, false
	}

	// Fallback to checking SQLite
	var ki KeyInfo
	var isAct int
	err := m.db.QueryRow("SELECT id, key, prefix, name, is_active FROM api_keys WHERE key = ? LIMIT 1", rawKey).
		Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct)
	if err != nil {
		return nil, false
	}

	ki.IsActive = (isAct == 1)
	ki.Key = MaskKey(ki.RawKey)

	m.mu.Lock()
	m.cache[rawKey] = &ki
	m.mu.Unlock()

	if ki.IsActive {
		return &ki, true
	}
	return nil, false
}

// ResolveKeyName returns the key name for traffic logging
func (m *Manager) ResolveKeyName(rawKey string) string {
	ki, ok := m.ValidateClientKey(rawKey)
	if ok && ki != nil {
		return ki.Name
	}
	return ""
}
