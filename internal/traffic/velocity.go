package traffic

import (
	"database/sql"
	"fmt"
	"time"

	"nineguard/internal/timeutil"
)

type VelocityStats struct {
	Tokens24h   int64 `json:"tokens_24h"`
	Tokens7dAvg int64 `json:"tokens_7d_avg"`
	Tokens30d   int64 `json:"tokens_30d"`
}

// GetVelocityStats calculates token consumption over past 24h, 7d daily average, and 30d total.
func (m *Manager) GetVelocityStats(apiKeyID string, now time.Time) (*VelocityStats, error) {
	if m == nil || m.db == nil {
		return &VelocityStats{}, nil
	}

	now = now.UTC()
	t24h := now.Add(-24 * time.Hour).Format(timeutil.SQLiteLayout)
	t7d := now.Add(-7 * 24 * time.Hour).Format(timeutil.SQLiteLayout)
	t30d := now.Add(-30 * 24 * time.Hour).Format(timeutil.SQLiteLayout)

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN timestamp >= ? THEN total_tokens ELSE 0 END), 0) AS tok_24h,
			COALESCE(SUM(CASE WHEN timestamp >= ? THEN total_tokens ELSE 0 END), 0) AS tok_7d,
			COALESCE(SUM(CASE WHEN timestamp >= ? THEN total_tokens ELSE 0 END), 0) AS tok_30d,
			MIN(timestamp) AS first_seen
		FROM traffic_logs
		WHERE api_key_id = ? AND timestamp >= ?
	`

	var tok24h, tok7d, tok30d int64
	var firstSeen sql.NullString
	err := m.db.QueryRow(query, t24h, t7d, t30d, apiKeyID, t30d).Scan(&tok24h, &tok7d, &tok30d, &firstSeen)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query velocity stats: %w", err)
	}

	daysActive := 7
	if firstSeen.Valid && firstSeen.String != "" {
		if tFirst, parseErr := time.Parse(timeutil.SQLiteLayout, firstSeen.String); parseErr == nil {
			d := int(now.Sub(tFirst).Hours()/24) + 1
			if d >= 1 && d < 7 {
				daysActive = d
			}
		}
	}

	return &VelocityStats{
		Tokens24h:   tok24h,
		Tokens7dAvg: tok7d / int64(daysActive),
		Tokens30d:   tok30d,
	}, nil
}
