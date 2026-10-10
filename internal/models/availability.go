package models

import (
	"strings"

	"nineguard/internal/db"
)

// Availability answers whether a Model Group / key entry names a Removed Model.
// It is a snapshot of the models table: ids that are present and ids that are removed.
type Availability struct {
	present []string
	removed []string
}

// LoadAvailability reads the present and removed model ids.
func LoadAvailability(database *db.DB) (*Availability, error) {
	rows, err := database.Query("SELECT id, removed_at IS NOT NULL FROM models")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	a := &Availability{}
	for rows.Next() {
		var id string
		var isRemoved bool
		if err := rows.Scan(&id, &isRemoved); err != nil {
			return nil, err
		}
		if isRemoved {
			a.removed = append(a.removed, id)
		} else {
			a.present = append(a.present, id)
		}
	}
	return a, rows.Err()
}

// entryMatchesID reports whether a literal entry names a model id. A bare entry
// ("gpt-4o") names "provider/gpt-4o", same as the key access check.
func entryMatchesID(entry, id string) bool {
	if strings.EqualFold(entry, id) {
		return true
	}
	if !strings.Contains(entry, "/") {
		if i := strings.Index(id, "/"); i != -1 && strings.EqualFold(entry, id[i+1:]) {
			return true
		}
	}
	return false
}

// IsUnavailable is true for a literal entry that matches a Removed Model and no present model.
// Wildcards, "all" and entries unknown to the models table are never unavailable.
func (a *Availability) IsUnavailable(entry string) bool {
	if a == nil {
		return false
	}
	entry = strings.TrimSpace(entry)
	if entry == "" || strings.Contains(entry, "*") || strings.EqualFold(entry, "all") {
		return false
	}
	for _, id := range a.present {
		if entryMatchesID(entry, id) {
			return false
		}
	}
	for _, id := range a.removed {
		if entryMatchesID(entry, id) {
			return true
		}
	}
	return false
}

// FilterAvailable returns entries without the unavailable ones.
func (a *Availability) FilterAvailable(entries []string) []string {
	if a == nil || len(a.removed) == 0 {
		return entries
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !a.IsUnavailable(e) {
			out = append(out, e)
		}
	}
	return out
}

// IsModelRemoved reports whether a requested model name is a Removed Model (ADR 0006).
// Errors read as "not removed": the proxy must never block or rewrite on a db failure.
func (m *Manager) IsModelRemoved(name string) bool {
	a, err := LoadAvailability(m.db)
	if err != nil {
		return false
	}
	return a.IsUnavailable(name)
}

// IsRemovedModel is true when id names a Removed Model and no present model.
func (a *Availability) IsRemovedModel(id string) bool {
	return a.IsUnavailable(id)
}
