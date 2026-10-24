package traffic

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
)

// CalcQuotaWindow returns the start of the current Quota Window and the next reset instant,
// both absolute instants. An active Transitional Window for the period wins; otherwise the
// window comes from the rule's schedule, and the zero ResetRule keeps the ADR 0005 UTC calendar.
func CalcQuotaWindow(period string, rule ResetRule, now time.Time) (start time.Time, resetAt time.Time) {
	if pin, ok := rule.Pins[period]; ok && !now.Before(pin.Start) && now.Before(pin.Until) {
		return pin.Start.UTC(), pin.Until.UTC()
	}
	return scheduleWindow(period, rule.Schedule, now)
}

// defaultWindow is the ADR 0005 behaviour: UTC calendar boundaries.
func defaultWindow(period string, now time.Time) (start time.Time, resetAt time.Time) {
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
func (m *Manager) GetQuotaUsage(apiKeyID string, period string, rule ResetRule, now time.Time) (int64, time.Time, error) {
	if m == nil || m.db == nil {
		return 0, time.Time{}, nil
	}
	return GetQuotaUsage(m.db, apiKeyID, period, rule, now)
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

// IsChatCommandsEnabled checks if slash commands in chat completions are enabled.
func (m *Manager) IsChatCommandsEnabled() bool {
	if m == nil || m.db == nil {
		return true
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'chat_commands_enabled' LIMIT 1").Scan(&val)
	if err != nil {
		return true
	}
	return strings.ToLower(strings.TrimSpace(val)) == "true"
}

// SetChatCommandsEnabled updates the chat commands enabled setting.
func (m *Manager) SetChatCommandsEnabled(enabled bool) error {
	if m == nil || m.db == nil {
		return nil
	}
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('chat_commands_enabled', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}

// GetUpstreamSyncInterval returns the stored upstream quota sync interval string (default "10m").
// Validation lives in the upstream package; callers parse the value there.
func (m *Manager) GetUpstreamSyncInterval() string {
	if m == nil || m.db == nil {
		return "10m"
	}
	var val string
	if err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'upstream_quota_sync_interval' LIMIT 1").Scan(&val); err != nil || strings.TrimSpace(val) == "" {
		return "10m"
	}
	return strings.TrimSpace(val)
}

// SetUpstreamSyncInterval stores an already validated interval string.
func (m *Manager) SetUpstreamSyncInterval(val string) error {
	if m == nil || m.db == nil {
		return nil
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('upstream_quota_sync_interval', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}

// IsStreamUsageInjectionEnabled checks if proxy core should inject stream_options.include_usage.
func (m *Manager) IsStreamUsageInjectionEnabled() bool {
	if m == nil || m.db == nil {
		return true
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'stream_usage_injection' LIMIT 1").Scan(&val)
	if err != nil {
		return true
	}
	return strings.ToLower(strings.TrimSpace(val)) == "true"
}

// SetStreamUsageInjectionEnabled updates the stream_usage_injection setting.
func (m *Manager) SetStreamUsageInjectionEnabled(enabled bool) error {
	if m == nil || m.db == nil {
		return nil
	}
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('stream_usage_injection', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}

// GetSessionGapMinutes returns the inactivity gap in minutes to infer a new session (default 30).
func (m *Manager) GetSessionGapMinutes() int {
	if m == nil || m.db == nil {
		return 30
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'session_gap_minutes' LIMIT 1").Scan(&val)
	if err != nil {
		return 30
	}
	var mins int
	if _, err := fmt.Sscanf(val, "%d", &mins); err == nil && mins >= 1 {
		return mins
	}
	return 30
}

// SetSessionGapMinutes sets the session gap threshold in minutes.
func (m *Manager) SetSessionGapMinutes(mins int) error {
	if m == nil || m.db == nil {
		return nil
	}
	if mins < 1 {
		mins = 1
	}
	val := fmt.Sprintf("%d", mins)
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('session_gap_minutes', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}

// IsUsageComparisonEnabled returns whether the book comparison is enabled (default false).
func (m *Manager) IsUsageComparisonEnabled() bool {
	if m == nil || m.db == nil {
		return false
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'usage_comparison_enabled' LIMIT 1").Scan(&val)
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(val)) == "true"
}

// SetUsageComparisonEnabled toggles the usage comparison feature.
func (m *Manager) SetUsageComparisonEnabled(enabled bool) error {
	if m == nil || m.db == nil {
		return nil
	}
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('usage_comparison_enabled', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}

// GetQuotaUsage queries cumulative tokens consumed by apiKeyID within its active quota window.
func GetQuotaUsage(d *db.DB, apiKeyID string, period string, rule ResetRule, now time.Time) (int64, time.Time, error) {
	if period == "none" || period == "" {
		return 0, time.Time{}, nil
	}
	start, resetAt := CalcQuotaWindow(period, rule, now)
	startStr := start.UTC().Format(timeutil.SQLiteLayout)

	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM traffic_logs WHERE api_key_id = ? AND timestamp >= ?`
	var consumed int64
	err := d.QueryRow(query, apiKeyID, startStr).Scan(&consumed)
	if err != nil && err != sql.ErrNoRows {
		return 0, resetAt, fmt.Errorf("query quota usage: %w", err)
	}
	return consumed, resetAt, nil
}
