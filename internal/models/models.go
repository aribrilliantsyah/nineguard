package models

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/providers"
	"nineguard/internal/timeutil"
)

type ModelInfo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ProviderID    string    `json:"provider_id"`
	Enabled       bool      `json:"enabled"`
	Removed       bool      `json:"removed"`
	RemovedAt     *string   `json:"removed_at,omitempty"`
	TotalRequests int       `json:"total_requests"`
	TotalTokens   int       `json:"total_tokens"`
	LastUsedAt    *string   `json:"last_used_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Manager struct {
	db *db.DB

	// pendingRemoval holds providers whose last sync would have removed most models.
	// In memory only: a restart just delays the removal by one sync (ADR 0006).
	mu             sync.Mutex
	pendingRemoval map[string]bool

	onSynced func()

	// Sync outcome for the dashboard (in memory, resets on restart).
	lastSyncAt    time.Time
	lastSyncError string
}

// SyncStatus is the outcome of the most recent sync attempt.
type SyncStatus struct {
	// LastSuccessAt is zero until a sync has succeeded since startup.
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	// PendingProviders lists providers whose removal waits for a confirming sync.
	PendingProviders []string `json:"pending_providers"`
}

// GetSyncStatus reports when sync last succeeded and which providers are in the mass-removal guard.
func (m *Manager) GetSyncStatus() SyncStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := SyncStatus{LastError: m.lastSyncError, PendingProviders: []string{}}
	if !m.lastSyncAt.IsZero() {
		t := m.lastSyncAt
		s.LastSuccessAt = &t
	}
	for p, pending := range m.pendingRemoval {
		if pending {
			s.PendingProviders = append(s.PendingProviders, p)
		}
	}
	sort.Strings(s.PendingProviders)
	return s
}

// SetOnSynced registers a callback run after every sync, so caches built from
// model availability (key allow lists) can refresh.
func (m *Manager) SetOnSynced(fn func()) {
	m.mu.Lock()
	m.onSynced = fn
	m.mu.Unlock()
}

func NewManager(database *db.DB) *Manager {
	return &Manager{
		db: database,
	}
}

// IsModelEnabled checks if a model is allowed. Returns true by default if not explicitly disabled.
func (m *Manager) IsModelEnabled(modelID string) bool {
	var enabled int
	err := m.db.QueryRow("SELECT enabled FROM models WHERE id = ?", modelID).Scan(&enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			// Auto-register model as enabled with detected provider prefix
			provID := ""
			if idx := strings.Index(modelID, "/"); idx != -1 {
				provID = modelID[:idx]
			}
			_, _ = m.db.Exec("INSERT OR IGNORE INTO models (id, name, provider_id, enabled) VALUES (?, ?, ?, 1)", modelID, modelID, provID)
			return true
		}
		return true // default to allow if db error
	}
	return enabled == 1
}

// SetModelEnabled toggles or sets model status.
func (m *Manager) SetModelEnabled(modelID string, enabled bool) error {
	enInt := 0
	if enabled {
		enInt = 1
	}
	provID := ""
	if idx := strings.Index(modelID, "/"); idx != -1 {
		provID = modelID[:idx]
	}

	_, err := m.db.Exec(`
		INSERT INTO models (id, name, provider_id, enabled, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled, updated_at = CURRENT_TIMESTAMP
	`, modelID, modelID, provID, enInt)
	return err
}

// DeleteModel removes a model record from NineGuard's local database.
func (m *Manager) DeleteModel(modelID string) error {
	_, err := m.db.Exec("DELETE FROM models WHERE id = ?", modelID)
	return err
}

// ClearRemovedModels hard-deletes every Removed Model record and returns how many went.
// Traffic rows keep their model name but lose the "removed" tag (ADR 0006).
func (m *Manager) ClearRemovedModels() (int, error) {
	res, err := m.db.Exec("DELETE FROM models WHERE removed_at IS NOT NULL")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListModels returns all registered models with usage statistics, optionally filtered by provider prefix.
func (m *Manager) ListModels(providerFilter string) ([]ModelInfo, error) {
	whereClause := ""
	var args []interface{}
	if providerFilter != "" {
		whereClause = "WHERE m.provider_id = ? OR m.id LIKE ?"
		args = append(args, providerFilter, providerFilter+"/%")
	}

	query := fmt.Sprintf(`
		SELECT 
			m.id, 
			m.name, 
			COALESCE(m.provider_id, ''),
			m.enabled, 
			m.removed_at,
			m.created_at, 
			m.updated_at,
			COUNT(t.id) as total_requests,
			COALESCE(SUM(t.total_tokens), 0) as total_tokens,
			MAX(t.timestamp) as last_used_at
		FROM models m
		LEFT JOIN traffic_logs t ON m.id = t.model
		%s
		GROUP BY m.id
		ORDER BY m.enabled DESC, total_requests DESC, m.id ASC
	`, whereClause)

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ModelInfo
	for rows.Next() {
		var mi ModelInfo
		var enInt int
		var lastUsed, removedAt sql.NullString
		if err := rows.Scan(
			&mi.ID,
			&mi.Name,
			&mi.ProviderID,
			&enInt,
			&removedAt,
			&mi.CreatedAt,
			&mi.UpdatedAt,
			&mi.TotalRequests,
			&mi.TotalTokens,
			&lastUsed,
		); err != nil {
			return nil, err
		}
		mi.Enabled = (enInt == 1)
		mi.RemovedAt = timeutil.NullTimeString(removedAt)
		mi.Removed = removedAt.Valid
		if mi.ProviderID == "" {
			if idx := strings.Index(mi.ID, "/"); idx != -1 {
				mi.ProviderID = mi.ID[:idx]
			}
		}
		mi.LastUsedAt = timeutil.NullTimeString(lastUsed)
		list = append(list, mi)
	}
	return list, nil
}

// SyncFromProviders fetches models from all active upstream providers and applies the listing.
func (m *Manager) SyncFromProviders(ctx context.Context, pm *providers.Manager) (added int, removed int, err error) {
	if pm == nil {
		return 0, 0, fmt.Errorf("providers manager not configured")
	}

	modelsList, err := pm.AggregateModels(ctx)
	if err != nil {
		m.mu.Lock()
		m.lastSyncError = err.Error()
		m.mu.Unlock()
		return 0, 0, err
	}
	added, removed = m.applyListing(modelsList)
	m.mu.Lock()
	m.lastSyncAt = time.Now()
	m.lastSyncError = ""
	fn := m.onSynced
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
	return added, removed, nil
}

// massRemovalRatio is the share of a provider's present models that one sync may mark
// removed before the change is held back until the next sync confirms it (ADR 0006).
const massRemovalRatio = 0.5

// applyListing reconciles the models table with an aggregated upstream listing.
//   - Models in the listing are registered, and their removed_at flag is cleared.
//   - For each provider present in the listing, present models missing from it are marked removed,
//     unless that would mark more than massRemovalRatio of them; then the removal waits for a
//     second consecutive sync that agrees. Providers absent from the listing are left untouched.
//   - Only orphan rows (no provider, or a provider that no longer exists) are hard-deleted.
//     Deactivating a provider keeps its models.
func (m *Manager) applyListing(modelsList []map[string]interface{}) (added int, removed int) {
	// 1. Hard-delete orphans: no provider, or a provider that no longer exists.
	delRes, _ := m.db.Exec(`
		DELETE FROM models
		WHERE provider_id = ''
		   OR (
				provider_id NOT IN (SELECT id FROM providers)
				AND provider_id NOT IN (SELECT prefix FROM providers)
		   )
	`)
	if delRes != nil {
		if n, _ := delRes.RowsAffected(); n > 0 {
			removed += int(n)
		}
	}

	validModelIDs := make(map[string]bool)
	syncedProviders := make(map[string]bool)

	// 2. Register everything upstream returned; a returning model clears removed_at.
	for _, item := range modelsList {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		provID, _ := item["provider"].(string)
		if provID == "" {
			if idx := strings.Index(id, "/"); idx != -1 {
				provID = id[:idx]
			}
		}

		validModelIDs[id] = true
		if provID != "" {
			syncedProviders[provID] = true
		}

		res, err := m.db.Exec(`
			INSERT INTO models (id, name, provider_id, enabled, created_at, updated_at)
			VALUES (?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(id) DO UPDATE SET provider_id = excluded.provider_id, removed_at = NULL, updated_at = CURRENT_TIMESTAMP
		`, id, id, provID)
		if err == nil {
			n, _ := res.RowsAffected()
			if n > 0 {
				added++
			}
		}
	}

	// 3. Mark models a synced provider no longer lists. Rows are kept.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendingRemoval == nil {
		m.pendingRemoval = make(map[string]bool)
	}
	for provID := range syncedProviders {
		rows, err := m.db.Query(
			"SELECT id FROM models WHERE (provider_id = ? OR id LIKE ?) AND removed_at IS NULL",
			provID, provID+"/%")
		if err != nil {
			continue
		}
		var present, missing []string
		for rows.Next() {
			var mID string
			if err := rows.Scan(&mID); err == nil {
				present = append(present, mID)
				if !validModelIDs[mID] {
					missing = append(missing, mID)
				}
			}
		}
		rows.Close()

		if len(missing) == 0 {
			delete(m.pendingRemoval, provID)
			continue
		}
		if float64(len(missing)) > massRemovalRatio*float64(len(present)) && !m.pendingRemoval[provID] {
			slog.Warn("model sync would mark most of a provider's models removed; waiting for next sync to confirm",
				"provider", provID, "missing", len(missing), "present", len(present))
			m.pendingRemoval[provID] = true
			continue
		}
		delete(m.pendingRemoval, provID)

		for _, id := range missing {
			res, _ := m.db.Exec("UPDATE models SET removed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND removed_at IS NULL", id)
			if res != nil {
				if n, _ := res.RowsAffected(); n > 0 {
					removed += int(n)
				}
			}
		}
	}

	return added, removed
}
