package keys

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"nineguard/internal/traffic"
)

// ScheduleCounts says how many API Keys follow the Global Reset Schedule and how many hold their
// own. Inactive keys count: they can be re-enabled.
type ScheduleCounts struct {
	Following int `json:"following_keys"`
	Custom    int `json:"custom_keys"`
}

// GetGlobalSchedule returns a copy of the Global Reset Schedule (nil = unset).
func (m *Manager) GetGlobalSchedule() *traffic.ResetSchedule {
	g := m.globalSchedule()
	if g == nil {
		return nil
	}
	c := *g
	return &c
}

// ScheduleCounts counts keys that follow the global schedule and keys with their own.
func (m *Manager) ScheduleCounts() (ScheduleCounts, error) {
	var total, custom int
	if err := m.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN COALESCE(quota_reset_schedule,'') <> '' THEN 1 ELSE 0 END), 0) FROM api_keys`).
		Scan(&total, &custom); err != nil {
		return ScheduleCounts{}, err
	}
	return ScheduleCounts{Following: total - custom, Custom: custom}, nil
}

// keyScheduleRow is one api_keys row as the bulk writers need it.
type keyScheduleRow struct {
	id      string
	ownRaw  string
	pinsRaw string
}

// readScheduleRows loads every key's schedule columns and closes the cursor before returning:
// the database has a single connection, so nothing may be written on tx while rows are open.
func readScheduleRows(tx *sql.Tx) ([]keyScheduleRow, error) {
	rows, err := tx.Query(`SELECT id, COALESCE(quota_reset_schedule,''), COALESCE(quota_window_pins,'') FROM api_keys`)
	if err != nil {
		return nil, err
	}
	var out []keyScheduleRow
	for rows.Next() {
		var r keyScheduleRow
		if err := rows.Scan(&r.id, &r.ownRaw, &r.pinsRaw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	err = rows.Err()
	_ = rows.Close()
	return out, err
}

// SetGlobalSchedule sets (or, with nil, removes) the Global Reset Schedule. Every key without a
// schedule of its own gets the Transitional Window its change needs, so usage is kept (ADR 0010
// #5); keys with their own schedule are untouched. All of it is one transaction.
func (m *Manager) SetGlobalSchedule(s *traffic.ResetSchedule) (ScheduleCounts, error) {
	if s != nil {
		if err := s.Validate(); err != nil {
			return ScheduleCounts{}, err
		}
	}
	oldGlobal := m.globalSchedule()
	now := time.Now()

	tx, err := m.db.Begin() // from here on use ONLY tx: the database has a single connection
	if err != nil {
		return ScheduleCounts{}, err
	}
	defer tx.Rollback()

	rows, err := readScheduleRows(tx)
	if err != nil {
		return ScheduleCounts{}, err
	}
	if s == nil {
		_, err = tx.Exec(`DELETE FROM settings WHERE key = ?`, settingGlobalSchedule)
	} else {
		b, _ := json.Marshal(s)
		_, err = tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, settingGlobalSchedule, string(b))
	}
	if err != nil {
		return ScheduleCounts{}, err
	}

	for _, r := range rows {
		if parseSchedule(r.ownRaw, r.id) != nil {
			continue // custom schedule: not affected by the global one
		}
		pins := pinsAfterChange(oldGlobal, s, parsePins(r.pinsRaw, r.id), now)
		if _, err := tx.Exec(`UPDATE api_keys SET quota_window_pins = ? WHERE id = ?`, encodePins(pins), r.id); err != nil {
			return ScheduleCounts{}, fmt.Errorf("pin key %s: %w", r.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ScheduleCounts{}, err
	}

	m.mu.Lock()
	m.globalSched = s
	m.mu.Unlock()
	m.loadLocalCache()
	return m.ScheduleCounts()
}

// ClearScheduleOverrides removes every key's own schedule so all keys follow the global one.
// Each formerly custom key gets the Transitional Window its change needs; usage is untouched.
func (m *Manager) ClearScheduleOverrides() (ScheduleCounts, error) {
	global := m.globalSchedule()
	now := time.Now()

	tx, err := m.db.Begin() // only tx from here on
	if err != nil {
		return ScheduleCounts{}, err
	}
	defer tx.Rollback()

	rows, err := readScheduleRows(tx)
	if err != nil {
		return ScheduleCounts{}, err
	}
	for _, r := range rows {
		own := parseSchedule(r.ownRaw, r.id)
		if own == nil {
			if r.ownRaw == "" {
				continue // already follows the global schedule
			}
			// unreadable stored value: drop it, nothing was in effect so nothing to pin
			if _, err := tx.Exec(`UPDATE api_keys SET quota_reset_schedule = NULL WHERE id = ?`, r.id); err != nil {
				return ScheduleCounts{}, fmt.Errorf("clear key %s: %w", r.id, err)
			}
			continue
		}
		pins := pinsAfterChange(own, global, parsePins(r.pinsRaw, r.id), now)
		if _, err := tx.Exec(`UPDATE api_keys SET quota_reset_schedule = NULL, quota_window_pins = ? WHERE id = ?`, encodePins(pins), r.id); err != nil {
			return ScheduleCounts{}, fmt.Errorf("clear key %s: %w", r.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ScheduleCounts{}, err
	}
	m.loadLocalCache()
	return m.ScheduleCounts()
}
