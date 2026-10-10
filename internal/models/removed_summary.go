package models

// RemovedProviderSummary counts Removed Models for one provider.
type RemovedProviderSummary struct {
	ProviderID string   `json:"provider_id"`
	Count      int      `json:"count"`
	Sample     []string `json:"sample"`
}

// RemovedGroupSummary is a Model Group that holds Unavailable Entries.
type RemovedGroupSummary struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	UnavailableCount int      `json:"unavailable_count"`
	KeysCount        int      `json:"keys_count"`
	Unavailable      []string `json:"unavailable"`
}

// RemovedSummary is the dashboard view of Removed Models (ADR 0006).
type RemovedSummary struct {
	RemovedCount     int                      `json:"removed_count"`
	Providers        []RemovedProviderSummary `json:"providers"`
	UnavailableCount int                      `json:"unavailable_count"`
	Groups           []RemovedGroupSummary    `json:"groups"`
	// AffectedKeys is the number of distinct API keys linked to a group with Unavailable Entries.
	AffectedKeys int        `json:"affected_keys"`
	Sync         SyncStatus `json:"sync"`
}

const removedSampleSize = 3

// GetRemovedSummary gathers everything the dashboard card shows.
func (m *Manager) GetRemovedSummary() (*RemovedSummary, error) {
	s := &RemovedSummary{
		Providers: []RemovedProviderSummary{},
		Groups:    []RemovedGroupSummary{},
		Sync:      m.GetSyncStatus(),
	}

	// Collect rows fully before the next query: the database holds a single connection.
	rows, err := m.db.Query(`
		SELECT id, COALESCE(NULLIF(provider_id, ''), '') FROM models
		WHERE removed_at IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	byProvider := map[string]*RemovedProviderSummary{}
	var order []string
	for rows.Next() {
		var id, prov string
		if err := rows.Scan(&id, &prov); err != nil {
			rows.Close()
			return nil, err
		}
		if prov == "" {
			for i := 0; i < len(id); i++ {
				if id[i] == '/' {
					prov = id[:i]
					break
				}
			}
		}
		p, ok := byProvider[prov]
		if !ok {
			p = &RemovedProviderSummary{ProviderID: prov, Sample: []string{}}
			byProvider[prov] = p
			order = append(order, prov)
		}
		p.Count++
		if len(p.Sample) < removedSampleSize {
			p.Sample = append(p.Sample, id)
		}
		s.RemovedCount++
	}
	rows.Close()
	for _, prov := range order {
		s.Providers = append(s.Providers, *byProvider[prov])
	}

	groups, err := m.ListGroups()
	if err != nil {
		return nil, err
	}
	linkedKeys, err := m.keysByGroup()
	if err != nil {
		return nil, err
	}
	affected := map[string]bool{}
	for _, g := range groups {
		if g.UnavailableCount == 0 {
			continue
		}
		s.UnavailableCount += g.UnavailableCount
		s.Groups = append(s.Groups, RemovedGroupSummary{
			ID: g.ID, Name: g.Name, UnavailableCount: g.UnavailableCount,
			KeysCount: g.KeysCount, Unavailable: g.UnavailableModels,
		})
		for _, keyID := range linkedKeys[g.ID] {
			affected[keyID] = true
		}
	}
	s.AffectedKeys = len(affected)
	return s, nil
}

// keysByGroup maps group id to the ids of API keys in group mode that link it.
func (m *Manager) keysByGroup() (map[string][]string, error) {
	rows, err := m.db.Query("SELECT id, COALESCE(model_group_ids, '[]') FROM api_keys WHERE model_access_mode = 'group'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var keyID, raw string
		if err := rows.Scan(&keyID, &raw); err != nil {
			return nil, err
		}
		for _, gid := range ParseJSONStringArray(raw) {
			out[gid] = append(out[gid], keyID)
		}
	}
	return out, rows.Err()
}
