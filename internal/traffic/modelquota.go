package traffic

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
)

// GetModelQuotaUsage sums tokens a key used on one resolved Model ID inside the period's current Quota Window (the key's schedule and Transitional Windows, ADR 0010).
func GetModelQuotaUsage(d *db.DB, keyID, modelID, period string, rule ResetRule, now time.Time) (int64, time.Time, error) {
	start, resetAt := CalcQuotaWindow(period, rule, now)
	var n int64
	err := d.QueryRow(`SELECT COALESCE(SUM(total_tokens),0) FROM traffic_logs WHERE api_key_id = ? AND model_id = ? AND timestamp >= ?`,
		keyID, modelID, start.Format(timeutil.SQLiteLayout)).Scan(&n)
	if err != nil {
		return 0, resetAt, fmt.Errorf("query model quota usage: %w", err)
	}
	return n, resetAt, nil
}

func (m *Manager) GetModelQuotaUsage(keyID, modelID, period string, rule ResetRule, now time.Time) (int64, time.Time, error) {
	if m == nil || m.db == nil {
		return 0, time.Time{}, nil
	}
	return GetModelQuotaUsage(m.db, keyID, modelID, period, rule, now)
}

// GetModelUsageSince sums tokens per model_id since start (keys lowercased).
// One query per key and window, however many Model Quotas the key has.
func GetModelUsageSince(d *db.DB, keyID string, start time.Time) (map[string]int64, error) {
	rows, err := d.Query(`SELECT model_id, COALESCE(SUM(total_tokens),0) FROM traffic_logs
		WHERE api_key_id = ? AND model_id IS NOT NULL AND timestamp >= ? GROUP BY model_id`,
		keyID, start.UTC().Format(timeutil.SQLiteLayout)) // stored timestamps are UTC
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[strings.ToLower(id)] += n
	}
	return out, rows.Err()
}

// GetModelVelocityStats is approximate (ADR 0008): rows with a model_id match it exactly;
// older rows (model_id NULL) match by raw model name (modelID or bareName).
func (m *Manager) GetModelVelocityStats(keyID, modelID, bareName string, now time.Time) (*VelocityStats, error) {
	if m == nil || m.db == nil {
		return &VelocityStats{}, nil
	}
	if bareName == "" {
		bareName = modelID
	}
	where := ` AND (model_id = ? OR (model_id IS NULL AND (model = ? COLLATE NOCASE OR model = ? COLLATE NOCASE)))`
	return m.velocity(keyID, where, []any{modelID, modelID, bareName}, now)
}

// GetQuotaWarnPercent is the system-wide Near-Limit threshold (default 80).
func GetQuotaWarnPercent(d *db.DB) int {
	var v string
	if err := d.QueryRow(`SELECT value FROM settings WHERE key='quota_warn_percent' LIMIT 1`).Scan(&v); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			return n
		}
	}
	return 80
}

func (m *Manager) GetQuotaWarnPercent() int {
	if m == nil || m.db == nil {
		return 80
	}
	return GetQuotaWarnPercent(m.db)
}

func (m *Manager) SetQuotaWarnPercent(p int) error {
	if p < 1 || p > 100 {
		return fmt.Errorf("quota_warn_percent must be between 1 and 100")
	}
	_, err := m.db.Exec(`INSERT INTO settings (key,value,updated_at) VALUES ('quota_warn_percent',?,CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, strconv.Itoa(p))
	return err
}
