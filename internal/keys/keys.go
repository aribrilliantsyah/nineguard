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
	"nineguard/internal/models"
	"nineguard/internal/timeutil"
	"nineguard/internal/traffic"
	"nineguard/internal/vault"
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
	QuotaLimit      int64     `json:"quota_limit"`
	QuotaPeriod     string    `json:"quota_period"`
	QuotaUsage      int64     `json:"quota_usage"`
	// QuotaResetAt is when the Key Quota's current window ends (absolute UTC instant, ADR 0010);
	// nil when the key has no resetting Key Quota (none or total).
	QuotaResetAt *string `json:"quota_reset_at"`
	QuotaMode       string    `json:"quota_mode"` // "key" (Key Quota) or "model" (Model Quotas), ADR 0007
	// ModelQuotas is filled on single-key reads; list responses carry ModelQuotaSummary only.
	ModelQuotas       []ModelQuota       `json:"model_quotas,omitempty"`
	ModelQuotaSummary *ModelQuotaSummary `json:"model_quota_summary,omitempty"`

	// Quota Reset Schedule (ADR 0010). ResetSchedule is the key's own (nil = follows the global
	// schedule); EffectiveResetSchedule is what applies (nil = the UTC default). ResetRule is the
	// effective schedule plus Transitional Windows, ready for traffic.CalcQuotaWindow.
	ResetSchedule          *traffic.ResetSchedule `json:"quota_reset_schedule"`
	EffectiveResetSchedule *traffic.ResetSchedule `json:"effective_quota_reset_schedule"`
	ResetScheduleSource    string                 `json:"quota_reset_schedule_source"` // key | global | default
	ResetRule              traffic.ResetRule      `json:"-"`

	CostVisibility *string `json:"cost_visibility"` // "show", "hide", or nil (follows global setting)

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
			return ki.manager.availableOnly(ki.manager.GetModelsForGroups(ki.ModelGroupIDs))
		}
		return ki.AllowedModels
	case "custom":
		if ki.manager != nil {
			return ki.manager.availableOnly(ki.AllowedModels)
		}
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
	// 1. Key allows bare "gpt-4o", but request is "provider/gpt-4o"
	if strings.Contains(model, "/") && !strings.Contains(allowed, "/") {
		parts := strings.SplitN(model, "/", 2)
		if strings.EqualFold(parts[1], allowed) {
			return true
		}
	}
	// 2. Key allows "provider/gpt-4o", but request is bare "gpt-4o"
	if strings.Contains(allowed, "/") && !strings.Contains(model, "/") {
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
	avail       *models.Availability // which model ids are Removed Models (ADR 0006)
	globalSched *traffic.ResetSchedule // Global Reset Schedule (ADR 0010); nil = unset
}

// availableOnly drops entries that name a Removed Model.
func (m *Manager) availableOnly(entries []string) []string {
	m.mu.RLock()
	a := m.avail
	m.mu.RUnlock()
	return a.FilterAvailable(entries)
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
		dec, _ := vault.Decrypt(strings.TrimSpace(dbUpstream))
		m.upstreamKey = dec
	}

	// 2. Load Model Groups cache and the Global Reset Schedule
	m.ReloadGroupCache()
	m.ReloadGlobalSchedule()

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
	rows.Close() // release the single db connection before the next query

	avail, _ := models.LoadAvailability(m.db)

	m.mu.Lock()
	m.groupCache = newCache
	m.avail = avail
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
	enc, err := vault.Encrypt(key)
	if err != nil {
		enc = key
	}
	_, err = m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('router_api_key', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, enc)
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
	// Read Model Quotas first and close that query: the database has a single connection,
	// so a second query cannot start while another result set is still open.
	quotaRows, _ := loadModelQuotaRows(m.db, "")

	rows, err := m.db.Query(`
		SELECT id, key, prefix, name, is_active, 
		       COALESCE(model_access_mode, 'all'), 
		       COALESCE(model_group_ids, '[]'), 
		       COALESCE(allowed_models, ''),
		       COALESCE(quota_limit, 0),
		       COALESCE(quota_period, 'none'),
		       COALESCE(quota_mode, 'key'),
		       COALESCE(quota_reset_schedule, ''),
		       COALESCE(quota_window_pins, ''),
		       cost_visibility
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
		var mode, rawGroupIDs, rawModels, ownSched, rawPins string
		var costVis *string
		if err := rows.Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct, &mode, &rawGroupIDs, &rawModels, &ki.QuotaLimit, &ki.QuotaPeriod, &ki.QuotaMode, &ownSched, &rawPins, &costVis); err == nil {
			ki.CostVisibility = costVis
			ki.ModelQuotas = quotaRows[ki.ID]
			applyResetSchedule(&ki, ownSched, rawPins, m.globalSched)
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

type CreateKeyOptions struct {
	ModelAccessMode string
	ModelGroupIDs   []string
	AllowedModels   []string
	QuotaLimit      int64
	QuotaPeriod     string
	QuotaMode       string       // "" means "key"
	ModelQuotas     []ModelQuota // only model_id, quota_limit, quota_period are used

	// ResetSchedule is the key's own Quota Reset Schedule; nil means it follows the global
	// one. A new key has no usage history, so no Transitional Window is written.
	ResetSchedule *traffic.ResetSchedule

	CostVisibility *string // "show", "hide", or nil
}

// CreateKey issues a new NineGuard API key with customizable model access mode, groups, and allowed models
func (m *Manager) CreateKey(name, modelAccessMode string, modelGroupIDs, allowedModels []string) (*KeyInfo, error) {
	return m.CreateKeyWithOptions(name, CreateKeyOptions{
		ModelAccessMode: modelAccessMode,
		ModelGroupIDs:   modelGroupIDs,
		AllowedModels:   allowedModels,
		QuotaLimit:      0,
		QuotaPeriod:     "none",
	})
}

// CreateKeyWithOptions issues a new NineGuard API key with customizable options including quotas
func (m *Manager) CreateKeyWithOptions(name string, opts CreateKeyOptions) (*KeyInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Agent Key"
	}

	modelAccessMode := strings.ToLower(strings.TrimSpace(opts.ModelAccessMode))
	if modelAccessMode == "" {
		if len(opts.ModelGroupIDs) > 0 {
			modelAccessMode = "group"
		} else if len(opts.AllowedModels) > 0 {
			modelAccessMode = "custom"
		} else {
			modelAccessMode = "all"
		}
	}

	quotaPeriod := strings.ToLower(strings.TrimSpace(opts.QuotaPeriod))
	if quotaPeriod == "" {
		quotaPeriod = "none"
	}
	quotaLimit := opts.QuotaLimit
	if quotaLimit < 0 {
		quotaLimit = 0
	}

	rawKey, prefix, err := generateSecureToken("sk-ng-")
	if err != nil {
		return nil, fmt.Errorf("failed to generate key token: %w", err)
	}

	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)

	serializedGroups := SerializeAllowedModels(opts.ModelGroupIDs)
	cleanGroups := ParseAllowedModels(serializedGroups)

	serializedModels := SerializeAllowedModels(opts.AllowedModels)
	cleanModels := ParseAllowedModels(serializedModels)

	quotaMode, ok := NormalizeQuotaMode(opts.QuotaMode)
	if !ok {
		return nil, fmt.Errorf("invalid quota_mode")
	}

	var costVis *string
	if opts.CostVisibility != nil {
		cv := strings.ToLower(strings.TrimSpace(*opts.CostVisibility))
		if cv == "show" || cv == "hide" {
			costVis = &cv
		}
	}

	now := time.Now()
	// Key row and Model Quota rows in one transaction (only tx inside: single connection).
	tx, err := m.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`
		INSERT INTO api_keys (id, key, prefix, name, is_active, model_access_mode, model_group_ids, allowed_models, quota_limit, quota_period, quota_mode, quota_reset_schedule, cost_visibility, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, rawKey, prefix, name, modelAccessMode, serializedGroups, serializedModels, quotaLimit, quotaPeriod, quotaMode, encodeSchedule(opts.ResetSchedule), costVis); err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}
	if err := replaceModelQuotas(tx, id, opts.ModelQuotas); err != nil {
		return nil, fmt.Errorf("failed to save new key: %w", err)
	}
	if err := tx.Commit(); err != nil {
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
		QuotaLimit:      quotaLimit,
		QuotaPeriod:     quotaPeriod,
		QuotaMode:       quotaMode,
		ModelQuotas:     append([]ModelQuota(nil), opts.ModelQuotas...),
		ResetSchedule:   opts.ResetSchedule,
		CostVisibility:  costVis,
		TotalRequests:   0,
		TotalTokens:     0,
		CreatedAt:       now,
		UpdatedAt:       now,
		manager:         m,
	}

	ki.EffectiveResetSchedule, ki.ResetScheduleSource = effectiveSchedule(ki.ResetSchedule, m.globalSchedule())
	ki.ResetRule = traffic.ResetRule{Schedule: ki.EffectiveResetSchedule}

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

// UpdateKeyQuota updates quota limit and period for a key
func (m *Manager) UpdateKeyQuota(id string, quotaLimit int64, quotaPeriod string) (*KeyInfo, error) {
	if quotaLimit < 0 {
		quotaLimit = 0
	}
	quotaPeriod = strings.ToLower(strings.TrimSpace(quotaPeriod))
	if quotaPeriod == "" {
		quotaPeriod = "none"
	}
	res, err := m.db.Exec(`
		UPDATE api_keys
		SET quota_limit = ?, quota_period = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, quotaLimit, quotaPeriod, id)
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
	var mode, rawGroupIDs, rawModels, ownSched, rawPins string
	var costVis *string
	err := m.db.QueryRow(`
		SELECT id, key, prefix, name, is_active, 
		       COALESCE(model_access_mode, 'all'), 
		       COALESCE(model_group_ids, '[]'), 
		       COALESCE(allowed_models, ''), 
		       COALESCE(quota_limit, 0), 
		       COALESCE(quota_period, 'none'), 
		       COALESCE(quota_mode, 'key'), 
		       COALESCE(quota_reset_schedule, ''), 
		       COALESCE(quota_window_pins, ''),
		       cost_visibility,
		       created_at, updated_at
		FROM api_keys WHERE id = ?
	`, id).Scan(&ki.ID, &rawKey, &ki.Prefix, &ki.Name, &isActiveInt, &mode, &rawGroupIDs, &rawModels, &ki.QuotaLimit, &ki.QuotaPeriod, &ki.QuotaMode, &ownSched, &rawPins, &costVis, &ki.CreatedAt, &ki.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ki.CostVisibility = costVis
	applyResetSchedule(&ki, ownSched, rawPins, m.globalSchedule())
	ki.ModelQuotas, _ = m.GetModelQuotas(id) // after QueryRow has released the connection
	ki.IsActive = (isActiveInt == 1)
	ki.Key = MaskKey(rawKey)
	ki.RawKey = rawKey
	ki.ModelAccessMode = mode
	ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
	ki.AllowedModels = ParseAllowedModels(rawModels)
	ki.manager = m
	if ki.QuotaLimit > 0 && ki.QuotaPeriod != "" && ki.QuotaPeriod != "none" {
		usage, resetAt, _ := traffic.GetQuotaUsage(m.db, ki.ID, ki.QuotaPeriod, ki.ResetRule, time.Now())
		ki.QuotaUsage = usage
		ki.QuotaResetAt = resetAtString(ki.QuotaPeriod, resetAt)
	}
	if len(ki.ModelQuotas) > 0 {
		m.AnnotateModelQuotas(&ki, traffic.GetQuotaWarnPercent(m.db), time.Now())
		if ki.QuotaMode == QuotaModeModel {
			ki.ModelQuotaSummary = SummarizeModelQuotas(ki.ModelQuotas)
		}
	}
	return &ki, nil
}

// keyStatsFrom is the FROM clause shared by ListKeys and ListKeysPage: every
// key with its all-time traffic stats, linked by traffic_logs.api_key_id.
const keyStatsFrom = `
	FROM api_keys k
	LEFT JOIN (
		SELECT api_key_id,
		       COUNT(*) AS total_requests,
		       COALESCE(SUM(total_tokens), 0) AS total_tokens,
		       MAX(timestamp) AS last_used_at
		FROM traffic_logs
		WHERE api_key_id IS NOT NULL
		GROUP BY api_key_id
	) s ON s.api_key_id = k.id`

const keyStatsColumns = `
	k.id, k.key, k.prefix, k.name, k.is_active,
	COALESCE(k.model_access_mode, 'all'),
	COALESCE(k.model_group_ids, '[]'),
	COALESCE(k.allowed_models, ''),
	COALESCE(k.quota_limit, 0),
	COALESCE(k.quota_period, 'none'),
	COALESCE(k.quota_mode, 'key'),
	COALESCE(k.quota_reset_schedule, ''),
	COALESCE(k.quota_window_pins, ''),
	k.created_at, k.updated_at,
	COALESCE(s.total_requests, 0),
	COALESCE(s.total_tokens, 0),
	s.last_used_at`

func (m *Manager) scanKeyRows(rows *sql.Rows) ([]KeyInfo, error) {
	list := make([]KeyInfo, 0)
	for rows.Next() {
		var ki KeyInfo
		var rawKey string
		var isActiveInt int
		var mode, rawGroupIDs, rawModels, ownSched, rawPins string
		var lastUsed sql.NullString
		if err := rows.Scan(
			&ki.ID, &rawKey, &ki.Prefix, &ki.Name, &isActiveInt,
			&mode, &rawGroupIDs, &rawModels,
			&ki.QuotaLimit, &ki.QuotaPeriod, &ki.QuotaMode,
			&ownSched, &rawPins,
			&ki.CreatedAt, &ki.UpdatedAt,
			&ki.TotalRequests, &ki.TotalTokens, &lastUsed,
		); err != nil {
			return nil, err
		}
		applyResetSchedule(&ki, ownSched, rawPins, m.globalSchedule())
		ki.IsActive = (isActiveInt == 1)
		ki.Key = MaskKey(rawKey)
		ki.RawKey = rawKey
		ki.ModelAccessMode = mode
		ki.ModelGroupIDs = ParseAllowedModels(rawGroupIDs)
		ki.AllowedModels = ParseAllowedModels(rawModels)
		ki.manager = m
		ki.LastUsedAt = timeutil.NullTimeString(lastUsed)
		list = append(list, ki)
	}
	return list, rows.Err()
}

func (m *Manager) enrichQuotaUsage(list []KeyInfo) {
	now := time.Now().UTC()
	var quotaRows map[string][]ModelQuota // loaded once, only if a model-mode key is listed
	var warn int
	for i := range list {
		if list[i].QuotaLimit > 0 && list[i].QuotaPeriod != "" && list[i].QuotaPeriod != "none" {
			usage, resetAt, _ := traffic.GetQuotaUsage(m.db, list[i].ID, list[i].QuotaPeriod, list[i].ResetRule, now)
			list[i].QuotaUsage = usage
			list[i].QuotaResetAt = resetAtString(list[i].QuotaPeriod, resetAt)
		}
		if list[i].QuotaMode == QuotaModeModel {
			if quotaRows == nil {
				quotaRows, _ = loadModelQuotaRows(m.db, "")
				warn = traffic.GetQuotaWarnPercent(m.db)
			}
			// List responses carry the summary only; the full rows come from GET /keys/{id}.
			list[i].ModelQuotas = quotaRows[list[i].ID]
			m.AnnotateModelQuotas(&list[i], warn, now)
			list[i].ModelQuotaSummary = SummarizeModelQuotas(list[i].ModelQuotas)
			list[i].ModelQuotas = nil
		}
	}
}

// ListKeys returns all NineGuard API keys with all-time request and token
// stats, newest first. Used by the legacy (unpaged) GET /api/v1/keys.
func (m *Manager) ListKeys() ([]KeyInfo, error) {
	rows, err := m.db.Query("SELECT " + keyStatsColumns + keyStatsFrom + " ORDER BY k.created_at DESC, k.id")
	if err != nil {
		return nil, err
	}
	list, err := m.scanKeyRows(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	m.enrichQuotaUsage(list)
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

	// One transaction (use only tx: the database has a single connection) so a key never
	// outlives or orphans its Model Quotas.
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM api_keys WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM api_key_model_quotas WHERE api_key_id = ?", id); err != nil {
		return err
	}
	_, _ = tx.Exec("DELETE FROM plugin_bindings WHERE scope_type = 'key' AND scope_id = ?", id)
	if err := tx.Commit(); err != nil {
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
	var mode, rawGroupIDs, rawModels, ownSched, rawPins string
	var costVis *string
	err := m.db.QueryRow("SELECT id, key, prefix, name, is_active, COALESCE(model_access_mode, 'all'), COALESCE(model_group_ids, '[]'), COALESCE(allowed_models, ''), COALESCE(quota_limit, 0), COALESCE(quota_period, 'none'), COALESCE(quota_mode, 'key'), COALESCE(quota_reset_schedule, ''), COALESCE(quota_window_pins, ''), cost_visibility FROM api_keys WHERE key = ? LIMIT 1", rawKey).
		Scan(&ki.ID, &ki.RawKey, &ki.Prefix, &ki.Name, &isAct, &mode, &rawGroupIDs, &rawModels, &ki.QuotaLimit, &ki.QuotaPeriod, &ki.QuotaMode, &ownSched, &rawPins, &costVis)
	if err != nil {
		return nil, false
	}
	ki.CostVisibility = costVis
	ki.ModelQuotas, _ = m.GetModelQuotas(ki.ID)
	applyResetSchedule(&ki, ownSched, rawPins, m.globalSchedule())

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
