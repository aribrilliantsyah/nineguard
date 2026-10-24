package pricing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"nineguard/internal/db"
)

type repriceTarget struct {
	id               int64
	modelID          string
	promptTokens     int
	completionTokens int
	cacheReadTokens  int
	cacheWriteTokens int
}

// RepriceRange recomputes cost_usd for traffic rows in the given range per Spec §5.3.
// Skipping usage_estimated = 1, errors (status >= 400), and nineguard/system rows.
func RepriceRange(ctx context.Context, database *db.DB, resolver *Resolver, from, to time.Time, modelID string) (int, error) {
	if resolver == nil {
		return 0, fmt.Errorf("pricing resolver not initialized")
	}

	var conditions []string
	var args []any

	conditions = append(conditions, "usage_estimated = 0")
	conditions = append(conditions, "status_code < 400")
	conditions = append(conditions, "model != 'nineguard/system'")

	if !from.IsZero() {
		conditions = append(conditions, "timestamp >= datetime(?)")
		args = append(args, from.UTC().Format("2006-01-02 15:04:05"))
	}
	if !to.IsZero() {
		conditions = append(conditions, "timestamp <= datetime(?)")
		args = append(args, to.UTC().Format("2006-01-02 15:04:05"))
	}

	modelID = strings.TrimSpace(modelID)
	if modelID != "" {
		conditions = append(conditions, "COALESCE(NULLIF(model_id, ''), model) = ? COLLATE NOCASE")
		args = append(args, modelID)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(model_id, ''), model), prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens
		FROM traffic_logs
		%s
	`, whereClause)

	rows, err := database.Query(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to query traffic logs for reprice: %w", err)
	}

	var targets []repriceTarget
	for rows.Next() {
		var t repriceTarget
		if err := rows.Scan(&t.id, &t.modelID, &t.promptTokens, &t.completionTokens, &t.cacheReadTokens, &t.cacheWriteTokens); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close() // Release connection before transaction

	if len(targets) == 0 {
		return 0, nil
	}

	tx, err := database.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin reprice transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE traffic_logs SET cost_usd = ? WHERE id = ?")
	if err != nil {
		return 0, fmt.Errorf("failed to prepare reprice statement: %w", err)
	}
	defer stmt.Close()

	updatedCount := 0
	for _, t := range targets {
		costUSD := resolver.CalculateCost(t.modelID, t.promptTokens, t.completionTokens, t.cacheReadTokens, t.cacheWriteTokens)
		if _, err := stmt.Exec(costUSD, t.id); err != nil {
			return 0, fmt.Errorf("failed to update cost for traffic row %d: %w", t.id, err)
		}
		updatedCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit reprice transaction: %w", err)
	}

	slog.Info("re-priced traffic logs", "updated", updatedCount, "from", from, "to", to, "model", modelID, "source", "traffic")

	return updatedCount, nil
}
