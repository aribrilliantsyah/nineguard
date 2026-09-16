package models

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/providers"
)

type ModelInfo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ProviderID    string    `json:"provider_id"`
	Enabled       bool      `json:"enabled"`
	TotalRequests int       `json:"total_requests"`
	TotalTokens   int       `json:"total_tokens"`
	LastUsedAt    *string   `json:"last_used_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Manager struct {
	db           *db.DB
	routerTarget string
}

func NewManager(database *db.DB, routerTarget string) *Manager {
	return &Manager{
		db:           database,
		routerTarget: routerTarget,
	}
}

func (m *Manager) SetRouterTarget(target string) {
	m.routerTarget = strings.TrimRight(strings.TrimSpace(target), "/")
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
		var lastUsed sql.NullString
		if err := rows.Scan(
			&mi.ID,
			&mi.Name,
			&mi.ProviderID,
			&enInt,
			&mi.CreatedAt,
			&mi.UpdatedAt,
			&mi.TotalRequests,
			&mi.TotalTokens,
			&lastUsed,
		); err != nil {
			return nil, err
		}
		mi.Enabled = (enInt == 1)
		if mi.ProviderID == "" {
			if idx := strings.Index(mi.ID, "/"); idx != -1 {
				mi.ProviderID = mi.ID[:idx]
			}
		}
		if lastUsed.Valid {
			mi.LastUsedAt = &lastUsed.String
		}
		list = append(list, mi)
	}
	return list, nil
}

// SyncFromProviders fetches models from all active upstream providers, registers them with their prefix,
// and automatically deletes any models that have no provider or whose provider was removed.
func (m *Manager) SyncFromProviders(ctx context.Context, pm *providers.Manager) (added int, removed int, err error) {
	if pm == nil {
		return 0, 0, fmt.Errorf("providers manager not configured")
	}

	// 1. Automatically delete all models that have no provider or whose provider is not active
	delRes, _ := m.db.Exec(`
		DELETE FROM models 
		WHERE provider_id = '' 
		   OR (
				provider_id NOT IN (SELECT id FROM providers WHERE is_active = 1) 
				AND provider_id NOT IN (SELECT prefix FROM providers WHERE is_active = 1)
		   )
	`)
	if delRes != nil {
		if n, _ := delRes.RowsAffected(); n > 0 {
			removed += int(n)
		}
	}

	// 2. Fetch models from all active upstream providers
	modelsList, err := pm.AggregateModels(ctx)
	if err != nil {
		return 0, removed, err
	}

	validModelIDs := make(map[string]bool)
	syncedProviders := make(map[string]bool)

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
			ON CONFLICT(id) DO UPDATE SET provider_id = excluded.provider_id, updated_at = CURRENT_TIMESTAMP
		`, id, id, provID)
		if err == nil {
			n, _ := res.RowsAffected()
			if n > 0 {
				added++
			}
		}
	}

	// 3. For any provider that was synced, remove models in database that were NOT returned by upstream
	for provID := range syncedProviders {
		rows, err := m.db.Query("SELECT id FROM models WHERE provider_id = ? OR id LIKE ?", provID, provID+"/%")
		if err == nil {
			var toDelete []string
			for rows.Next() {
				var mID string
				if err := rows.Scan(&mID); err == nil {
					if !validModelIDs[mID] {
						toDelete = append(toDelete, mID)
					}
				}
			}
			rows.Close()

			for _, delID := range toDelete {
				res, _ := m.db.Exec("DELETE FROM models WHERE id = ?", delID)
				if res != nil {
					if n, _ := res.RowsAffected(); n > 0 {
						removed += int(n)
					}
				}
			}
		}
	}

	return added, removed, nil
}
