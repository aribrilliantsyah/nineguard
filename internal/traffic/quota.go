package traffic

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
)

// CalcQuotaWindow returns the UTC start instant and the next reset instant for a given quota period.
func CalcQuotaWindow(period string, now time.Time) (start time.Time, resetAt time.Time) {
	now = now.UTC()
	switch period {
	case "daily":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 0, 1)
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 { // Sunday in Go is 0, ISO Monday is 1
			weekday = 7
		}
		daysSinceMonday := weekday - 1
		start = time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 0, 7)
	case "monthly":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 1, 0)
	case "total":
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		resetAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	default:
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		resetAt = now
	}
	return start, resetAt
}

// GetQuotaUsage queries cumulative tokens consumed by apiKeyID within its active quota window.
func (m *Manager) GetQuotaUsage(apiKeyID string, period string, now time.Time) (int64, time.Time, error) {
	if m == nil || m.db == nil {
		return 0, time.Time{}, nil
	}
	return GetQuotaUsage(m.db, apiKeyID, period, now)
}

// GetHeavyTokenThreshold reads the configured heavy token threshold from settings (default 8000).
func (m *Manager) GetHeavyTokenThreshold() int {
	if m == nil || m.db == nil {
		return 8000
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'heavy_token_threshold' LIMIT 1").Scan(&val)
	if err == nil {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return 8000
}

// SetHeavyTokenThreshold updates the configured heavy token threshold in settings.
func (m *Manager) SetHeavyTokenThreshold(threshold int) error {
	if m == nil || m.db == nil {
		return nil
	}
	if threshold <= 0 {
		return fmt.Errorf("threshold must be greater than zero")
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('heavy_token_threshold', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, strconv.Itoa(threshold))
	return err
}

// GetQuotaUsage queries cumulative tokens consumed by apiKeyID within its active quota window.
func GetQuotaUsage(d *db.DB, apiKeyID string, period string, now time.Time) (int64, time.Time, error) {
	if period == "none" || period == "" {
		return 0, time.Time{}, nil
	}
	start, resetAt := CalcQuotaWindow(period, now)
	startStr := start.Format(timeutil.SQLiteLayout)

	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM traffic_logs WHERE api_key_id = ? AND timestamp >= ?`
	var consumed int64
	err := d.QueryRow(query, apiKeyID, startStr).Scan(&consumed)
	if err != nil && err != sql.ErrNoRows {
		return 0, resetAt, fmt.Errorf("query quota usage: %w", err)
	}
	return consumed, resetAt, nil
}
