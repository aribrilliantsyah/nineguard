package pricing_test

import (
	"context"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/pricing"
)

func TestRepriceRange(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	// Insert test traffic rows
	// Row 1: claude-3-5-sonnet, priced, not estimated
	_, err = database.Exec(`
		INSERT INTO traffic_logs (timestamp, model, model_id, prompt_tokens, completion_tokens, total_tokens, status_code, usage_estimated, cost_usd)
		VALUES (datetime('2026-10-10 10:00:00'), 'claude-3-5-sonnet', 'claude-3-5-sonnet', 1000, 500, 1500, 200, 0, NULL)
	`)
	if err != nil {
		t.Fatalf("failed to insert test row 1: %v", err)
	}

	// Row 2: estimated usage -> should be skipped by reprice
	_, err = database.Exec(`
		INSERT INTO traffic_logs (timestamp, model, model_id, prompt_tokens, completion_tokens, total_tokens, status_code, usage_estimated, cost_usd)
		VALUES (datetime('2026-10-10 10:05:00'), 'claude-3-5-sonnet', 'claude-3-5-sonnet', 1000, 500, 1500, 200, 1, NULL)
	`)
	if err != nil {
		t.Fatalf("failed to insert test row 2: %v", err)
	}

	// Row 3: status 500 -> should be skipped by reprice
	_, err = database.Exec(`
		INSERT INTO traffic_logs (timestamp, model, model_id, prompt_tokens, completion_tokens, total_tokens, status_code, usage_estimated, cost_usd)
		VALUES (datetime('2026-10-10 10:10:00'), 'claude-3-5-sonnet', 'claude-3-5-sonnet', 1000, 500, 1500, 500, 0, NULL)
	`)
	if err != nil {
		t.Fatalf("failed to insert test row 3: %v", err)
	}

	// Row 4: nineguard/system -> should be skipped by reprice
	_, err = database.Exec(`
		INSERT INTO traffic_logs (timestamp, model, model_id, prompt_tokens, completion_tokens, total_tokens, status_code, usage_estimated, cost_usd)
		VALUES (datetime('2026-10-10 10:15:00'), 'nineguard/system', 'nineguard/system', 100, 100, 200, 200, 0, NULL)
	`)
	if err != nil {
		t.Fatalf("failed to insert test row 4: %v", err)
	}

	resolver := pricing.NewResolver(map[string]pricing.Price{
		"claude-3-5-sonnet": {
			InputPerMTok:  3.0,
			OutputPerMTok: 15.0,
		},
	}, nil)

	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 10, 23, 59, 59, 0, time.UTC)

	updated, err := pricing.RepriceRange(context.Background(), database, resolver, from, to, "")
	if err != nil {
		t.Fatalf("RepriceRange failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("expected exactly 1 row re-priced, got %d", updated)
	}

	// Check that Row 1 now has cost_usd
	var costUSD *float64
	err = database.QueryRow("SELECT cost_usd FROM traffic_logs WHERE model = 'claude-3-5-sonnet' AND usage_estimated = 0").Scan(&costUSD)
	if err != nil {
		t.Fatalf("failed to query cost_usd: %v", err)
	}
	if costUSD == nil {
		t.Fatalf("expected cost_usd to be non-nil after re-price")
	}
	expected := (1000.0*3.0 + 500.0*15.0) / 1e6
	if *costUSD != expected {
		t.Fatalf("expected cost %v, got %v", expected, *costUSD)
	}

	// Check that Row 2 (estimated) still has NULL cost_usd
	err = database.QueryRow("SELECT cost_usd FROM traffic_logs WHERE usage_estimated = 1").Scan(&costUSD)
	if err != nil {
		t.Fatalf("failed to query cost_usd for estimated row: %v", err)
	}
	if costUSD != nil {
		t.Fatalf("expected cost_usd to remain nil for estimated row, got %v", *costUSD)
	}
}
