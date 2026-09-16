package models

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ModelGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Models      []string  `json:"models"`
	ModelsCount int       `json:"models_count"`
	KeysCount   int       `json:"keys_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func ParseJSONStringArray(raw string) []string {
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

func SerializeJSONStringArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	var clean []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it != "" {
			clean = append(clean, it)
		}
	}
	if len(clean) == 0 {
		return "[]"
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func generateGroupID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "grp_" + hex.EncodeToString(b)
}

// ListGroups returns all model groups with their models, models count, and count of linked API keys
func (m *Manager) ListGroups() ([]ModelGroup, error) {
	// Tally keys count per group ID
	keyCounts := make(map[string]int)
	kRows, err := m.db.Query("SELECT COALESCE(model_group_ids, '[]') FROM api_keys WHERE model_access_mode = 'group'")
	if err == nil {
		defer kRows.Close()
		for kRows.Next() {
			var rawGIDs string
			if err := kRows.Scan(&rawGIDs); err == nil {
				gids := ParseJSONStringArray(rawGIDs)
				for _, gid := range gids {
					keyCounts[gid]++
				}
			}
		}
	}

	rows, err := m.db.Query("SELECT id, name, description, models, created_at, updated_at FROM model_groups ORDER BY name ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query model groups: %w", err)
	}
	defer rows.Close()

	var groups []ModelGroup
	for rows.Next() {
		var g ModelGroup
		var rawModels string
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &rawModels, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		g.Models = ParseJSONStringArray(rawModels)
		g.ModelsCount = len(g.Models)
		g.KeysCount = keyCounts[g.ID]
		groups = append(groups, g)
	}

	return groups, nil
}

// GetGroup retrieves a single model group by ID
func (m *Manager) GetGroup(id string) (*ModelGroup, error) {
	var g ModelGroup
	var rawModels string
	err := m.db.QueryRow("SELECT id, name, description, models, created_at, updated_at FROM model_groups WHERE id = ?", id).
		Scan(&g.ID, &g.Name, &g.Description, &rawModels, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("model group not found")
		}
		return nil, err
	}
	g.Models = ParseJSONStringArray(rawModels)
	g.ModelsCount = len(g.Models)

	// Count linked keys
	var count int
	kRows, kErr := m.db.Query("SELECT COALESCE(model_group_ids, '[]') FROM api_keys WHERE model_access_mode = 'group'")
	if kErr == nil {
		defer kRows.Close()
		for kRows.Next() {
			var rawGIDs string
			if err := kRows.Scan(&rawGIDs); err == nil {
				for _, gid := range ParseJSONStringArray(rawGIDs) {
					if gid == id {
						count++
						break
					}
				}
			}
		}
	}
	g.KeysCount = count

	return &g, nil
}

// CreateGroup creates a new model group
func (m *Manager) CreateGroup(name, description string, modelsList []string) (*ModelGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	id := generateGroupID()
	serialized := SerializeJSONStringArray(modelsList)

	now := time.Now()
	_, err := m.db.Exec(`
		INSERT INTO model_groups (id, name, description, models, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, name, strings.TrimSpace(description), serialized)
	if err != nil {
		return nil, fmt.Errorf("failed to create model group: %w", err)
	}

	return &ModelGroup{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(description),
		Models:      ParseJSONStringArray(serialized),
		ModelsCount: len(modelsList),
		KeysCount:   0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// UpdateGroup updates an existing model group's name, description, and model list
func (m *Manager) UpdateGroup(id, name, description string, modelsList []string) (*ModelGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	serialized := SerializeJSONStringArray(modelsList)

	res, err := m.db.Exec(`
		UPDATE model_groups
		SET name = ?, description = ?, models = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, strings.TrimSpace(description), serialized, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update model group: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("model group not found")
	}

	return m.GetGroup(id)
}

// DeleteGroup deletes a model group, protecting against deletion if API keys are actively linked to it
func (m *Manager) DeleteGroup(id string) error {
	// Check if any keys are currently using this group
	var linkedKeyNames []string
	rows, err := m.db.Query("SELECT name, COALESCE(model_group_ids, '[]') FROM api_keys WHERE model_access_mode = 'group'")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var kName, rawGIDs string
			if err := rows.Scan(&kName, &rawGIDs); err == nil {
				for _, gid := range ParseJSONStringArray(rawGIDs) {
					if gid == id {
						linkedKeyNames = append(linkedKeyNames, kName)
						break
					}
				}
			}
		}
	}

	if len(linkedKeyNames) > 0 {
		sample := linkedKeyNames
		if len(sample) > 3 {
			sample = sample[:3]
		}
		return fmt.Errorf("cannot delete group: currently linked to %d API key(s) (%s). Please unlink it from those keys first", len(linkedKeyNames), strings.Join(sample, ", "))
	}

	res, err := m.db.Exec("DELETE FROM model_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete model group: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("model group not found")
	}

	return nil
}
