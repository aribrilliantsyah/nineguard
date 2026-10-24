package db_test

import (
	"testing"

	"nineguard/internal/db"
)

func TestCostMigration_Idempotent(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer database.Close()

	// Verify model_prices table exists
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM model_prices").Scan(&count)
	if err != nil {
		t.Fatalf("expected model_prices table to exist: %v", err)
	}

	// Verify model_price_overrides table exists
	err = database.QueryRow("SELECT COUNT(*) FROM model_price_overrides").Scan(&count)
	if err != nil {
		t.Fatalf("expected model_price_overrides table to exist: %v", err)
	}

	// Verify traffic_logs has cost_usd column
	var costUSD *float64
	err = database.QueryRow("SELECT cost_usd FROM traffic_logs LIMIT 1").Scan(&costUSD)
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("expected cost_usd column in traffic_logs: %v", err)
	}

	// Verify api_keys has cost_visibility column
	var costVis *string
	err = database.QueryRow("SELECT cost_visibility FROM api_keys LIMIT 1").Scan(&costVis)
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("expected cost_visibility column in api_keys: %v", err)
	}

	// Verify default settings
	var dispCur string
	err = database.QueryRow("SELECT value FROM settings WHERE key = 'display_currency'").Scan(&dispCur)
	if err != nil || dispCur != "USD" {
		t.Fatalf("expected default display_currency 'USD', got %q, err=%v", dispCur, err)
	}

	var costVisKeys string
	err = database.QueryRow("SELECT value FROM settings WHERE key = 'cost_visible_to_keys'").Scan(&costVisKeys)
	if err != nil || costVisKeys != "true" {
		t.Fatalf("expected default cost_visible_to_keys 'true', got %q, err=%v", costVisKeys, err)
	}
}
