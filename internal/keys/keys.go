package keys

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
)

type KeyInfo struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"` // masked key for display
	RawKey          string    `json:"raw_key,omitempty"`
	Prefix          string    `json:"prefix"`
	Name            string    `json:"name"`
	IsActive        bool      `json:"is_active"`
	ModelAccessMode string    `json:"model_access_mode"` // "all", "group", "custom"
	ModelGroupIDs   []string  `json:"model_group_ids"`
	AllowedModels   []string  `json:"allowed_models"`
	TotalRequests   int       `json:"total_requests"`
	TotalTokens     int       `json:"total_tokens"`
	LastUsedAt      *string   `json:"last_used_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	manager *Manager `json:"-"`
}

func ParseAllowedModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var list []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &list); err == nil {
			var clean []string
			for _, m := range list {
				m = strings.TrimSpace(m)
				if m != "" {
					clean = append(clean, m)
				}
			}
			return clean
		}
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			list = append(list, p)
		}
	}
	return list
}

func SerializeAllowedModels(models []string) string {
	if len(models) == 0 {
		return ""
	}
	var clean []string
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m != "" {
			clean = append(clean, m)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return strings.Join(clean, ",")
	}
	return string(b)
}

func (ki *KeyInfo) IsAllModelsAllowed() bool {
	if ki == nil {
		return true
	}
	mode := ki.ModelAccessMode
	if mode == "" {
		if len(ki.ModelGroupIDs) > 0 {
			mode = "group"
		} else if len(ki.AllowedModels) > 0 {
			mode = "custom"
		} else {
			mode = "all"
		}
	}

	switch mode {
	case "all":
		return true
	case "group":
		for _, m := range ki.GetEffectiveAllowedModels() {
			if m == "*" || strings.EqualFold(m, "all") {
				return true
			}
		}
		return false
	case "custom":
		if len(ki.AllowedModels) == 0 {
			return true
		}
		for _, m := range ki.AllowedModels {
			if m == "*" || strings.EqualFold(m, "all") {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func (ki *KeyInfo) GetEffectiveAllowedModels() []string {
	if ki == nil {
		return []string{"*"}
	}
	mode := ki.ModelAccessMode
	if mode == "" {
		if len(ki.ModelGroupIDs) > 0 {
			mode = "group"
		} else if len(ki.AllowedModels) > 0 {
			mode = "custom"
		} else {
			mode = "all"
		}
	}

	switch mode {
	case "all":
		return []string{"*"}
	case "group":
		if ki.manager != nil {
			return ki.manager.GetModelsForGroups(ki.ModelGroupIDs)
		}
		return ki.AllowedModels
	case "custom":
		return ki.AllowedModels
	default:
		return []string{"*"}
	}
}

func matchModelPattern(allowed, model string) bool {
	allowed = strings.TrimSpace(allowed)
	if allowed == "*" || allowed == "" || strings.EqualFold(allowed, "all") {
		return true
	}
	// Exact match (case-insensitive)
	if strings.EqualFold(allowed, model) {
		return true
	}
	// Wildcard match e.g. "prefix/*"
	if strings.HasSuffix(allowed, "/*") {
		prefix := strings.TrimSuffix(allowed, "/*")
		if strings.HasPrefix(strings.ToLower(model), strings.ToLower(prefix)+"/") {
			return true
		}
	}
	// Suffix match e.g. "*.flash"
	if strings.HasPrefix(allowed, "*.") {
		suffix := strings.TrimPrefix(allowed, "*")
		if strings.HasSuffix(strings.ToLower(model), strings.ToLower(suffix)) {
			return true
		}
	}
	// Provider prefix matching:
	// 1. Key allows "gpt-4o", but request is "provider/gpt-4o"
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		if strings.EqualFold(parts[1], allowed) {
			return true
		}
	}
	// 2. Key allows "provider/gpt-4o", but request is "gpt-4o"
	if strings.Contains(allowed, "/") {
		parts := strings.SplitN(allowed, "/", 2)
		if strings.EqualFold(parts[1], model) {
			return true
		}
	}
	return false
}

func (ki *KeyInfo) IsModelAllowed(model string) bool {
	if ki == nil || ki.IsAllModelsAllowed() {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	effective := ki.GetEffectiveAllowedModels()
	if len(effective) == 0 {
		return false
	}
	for _, allowed := range effective {
		if matchModelPattern(allowed, model) {
			return true
		}
	}
	return false
}

type Manager struct {
	db          *db.DB
	mu          sync.RWMutex
	upstreamKey string
	cache       map[string]*KeyInfo // rawKey -> KeyInfo
	groupCache  map[string][]string // groupID -> []models
}

func NewManager(database *db.DB, defaultRouterAPIKey string) *Manager {
	m := &Manager{
		db:          database,
		upstreamKey: strings.TrimSpace(defaultRouterAPIKey),
		cache:       make(map[string]*KeyInfo),
		groupCache:  make(map[string][]string),
	}

	// 1. Load Upstream 9router key from database settings (overrides env if set in UI)
	var dbUpstream string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'router_api_key' LIMIT 1").Scan(&dbUpstream)
	if err == nil && strings.TrimSpace(dbUpstream) != "" {
		m.upstreamKey = strings.TrimSpace(dbUpstream)
	}

	// 2. Load Model Groups cache
	m.ReloadGroupCache()

	// 3. Load NineGuard API keys from database into cache
	m.loadLocalCache()

	// 4. Ensure at least one default NineGuard API key exists if database is empty
	m.ensureDefaultKey()

	return m
}

// ReloadGroupCache refreshes the in-memory cache of model groups
func (m *Manager) ReloadGroupCache() {
	rows, err := m.db.Query("SELECT id, models FROM model_groups")
	if err != nil {
		return
	}
	defer rows.Close()

	newCache := make(map[string][]string)
	for rows.Next() {
		var id, rawModels string
		if err := rows.Scan(&id, &rawModels); err == nil {
			newCache[id] = ParseAllowedModels(rawModels)
		}
	}

	m.mu.Lock()
	m.groupCache = newCache
	m.mu.Unlock()
}

// GetModelsForGroups resolves the union of models for the given group IDs
func (m *Manager) GetModelsForGroups(groupIDs []string) []string {
	if len(groupIDs) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[string]bool)
	var result []string
	for _, gid := range groupIDs {
		if models, ok := m.groupCache[gid]; ok {
			for _, mod := range models {
				if !seen[mod] {
					seen[mod] = true
					result = append(result, mod)
				}
			}
		}
	}
	return result
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
	rows, err := m.db.Query(`
		SELECT id, key, prefix, name, is_active, 
		       COALESCE(model_access_mode, 'all'), 
		       COALESCE(model_group_ids, '[]'), 
		       COALESCE(allowed_models, '') 
		FROM api_keys
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	for rows.Next() {
		var ki KeyInfo
		var isAct int
		var mode, rawGroupIDs, rawModels string
		if err := rows.Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct, &mode, &rawGroupIDs, &rawModels); err == nil {
			ki.IsActive = (isAct == 1)
			ki.Key = MaskKey(ki.RawKey)
			ki.ModelAccessMode = mode
			ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
			ki.AllowedModels = ParseAllowedModels(rawModels)
			ki.manager = m
			if ki.ModelAccessMode == "" {
				if len(ki.AllowedModels) > 0 {
					ki.ModelAccessMode = "custom"
				} else {
					ki.ModelAccessMode = "all"
				}
			}
			m.cache[ki.RawKey] = &ki
		}
	}
}

func (m *Manager) ensureDefaultKey() {
	var count int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&count)
	if count == 0 {
		_, _ = m.CreateKey("Default Agent Key", "all", nil, nil)
		slog.Info("created initial default NineGuard API key")
	}
}

// CreateKey issues a new NineGuard API key with customizable model access mode, groups, and allowed models
func (m *Manager) CreateKey(name, modelAccessMode string, modelGroupIDs, allowedModels []string) (*KeyInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Agent Key"
	}

	modelAccessMode = strings.ToLower(strings.TrimSpace(modelAccessMode))
	if modelAccessMode == "" {
		if len(modelGroupIDs) > 0 {
			modelAccessMode = "group"
		} else if len(allowedModels) > 0 {
			modelAccessMode = "custom"
		} else {
			modelAccessMode = "all"
		}
	}

	rawKey, prefix, err := generateSecureToken("sk-ng-")
	if err != nil {
		return nil, fmt.Errorf("failed to generate key token: %w", err)
	}

	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)

	serializedGroups := SerializeAllowedModels(modelGroupIDs)
	cleanGroups := ParseAllowedModels(serializedGroups)

	serializedModels := SerializeAllowedModels(allowedModels)
	cleanModels := ParseAllowedModels(serializedModels)

	now := time.Now()
	_, err = m.db.Exec(`
		INSERT INTO api_keys (id, key, prefix, name, is_active, model_access_mode, model_group_ids, allowed_models, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, rawKey, prefix, name, modelAccessMode, serializedGroups, serializedModels)
	if err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}

	ki := &KeyInfo{
		ID:              id,
		Key:             MaskKey(rawKey),
		RawKey:          rawKey,
		Prefix:          prefix,
		Name:            name,
		IsActive:        true,
		ModelAccessMode: modelAccessMode,
		ModelGroupIDs:   cleanGroups,
		AllowedModels:   cleanModels,
		TotalRequests:   0,
		TotalTokens:     0,
		CreatedAt:       now,
		UpdatedAt:       now,
		manager:         m,
	}

	m.mu.Lock()
	m.cache[rawKey] = ki
	m.mu.Unlock()

	return ki, nil
}

// UpdateKey updates key name, model access mode, model groups, and allowed models
func (m *Manager) UpdateKey(id, name, modelAccessMode string, modelGroupIDs, allowedModels []string) (*KeyInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("key name cannot be empty")
	}

	modelAccessMode = strings.ToLower(strings.TrimSpace(modelAccessMode))
	if modelAccessMode == "" {
		if len(modelGroupIDs) > 0 {
			modelAccessMode = "group"
		} else if len(allowedModels) > 0 {
			modelAccessMode = "custom"
		} else {
			modelAccessMode = "all"
		}
	}

	serializedGroups := SerializeAllowedModels(modelGroupIDs)
	serializedModels := SerializeAllowedModels(allowedModels)

	res, err := m.db.Exec(`
		UPDATE api_keys
		SET name = ?, model_access_mode = ?, model_group_ids = ?, allowed_models = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, modelAccessMode, serializedGroups, serializedModels, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("api key not found")
	}

	m.loadLocalCache()

	return m.GetKey(id)
}

// GetKey retrieves a key by ID
func (m *Manager) GetKey(id string) (*KeyInfo, error) {
	var ki KeyInfo
	var rawKey string
	var isActiveInt int
	var mode, rawGroupIDs, rawModels string
	err := m.db.QueryRow(`
		SELECT id, key, prefix, name, is_active, 
		       COALESCE(model_access_mode, 'all'), 
		       COALESCE(model_group_ids, '[]'), 
		       COALESCE(allowed_models, ''), 
		       created_at, updated_at
		FROM api_keys WHERE id = ?
	`, id).Scan(&ki.ID, &rawKey, &ki.Prefix, &ki.Name, &isActiveInt, &mode, &rawGroupIDs, &rawModels, &ki.CreatedAt, &ki.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ki.IsActive = (isActiveInt == 1)
	ki.Key = MaskKey(rawKey)
	ki.RawKey = rawKey
	ki.ModelAccessMode = mode
	ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
	ki.AllowedModels = ParseAllowedModels(rawModels)
	ki.manager = m
	return &ki, nil
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
			COALESCE(k.model_access_mode, 'all'),
			COALESCE(k.model_group_ids, '[]'),
			COALESCE(k.allowed_models, ''),
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
		var mode, rawGroupIDs, rawModels string
		var lastUsed sql.NullString
		if err := rows.Scan(
			&ki.ID,
			&rawKey,
			&ki.Prefix,
			&ki.Name,
			&isActiveInt,
			&mode,
			&rawGroupIDs,
			&rawModels,
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
		ki.ModelAccessMode = mode
		ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
		ki.AllowedModels = ParseAllowedModels(rawModels)
		ki.manager = m
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
	var mode, rawGroupIDs, rawModels string
	err := m.db.QueryRow("SELECT id, key, prefix, name, is_active, COALESCE(model_access_mode, 'all'), COALESCE(model_group_ids, '[]'), COALESCE(allowed_models, '') FROM api_keys WHERE key = ? LIMIT 1", rawKey).
		Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct, &mode, &rawGroupIDs, &rawModels)
	if err != nil {
		return nil, false
	}

	ki.IsActive = (isAct == 1)
	ki.Key = MaskKey(ki.RawKey)
	ki.ModelAccessMode = mode
	ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
	ki.AllowedModels = ParseAllowedModels(rawModels)
	ki.manager = m

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
