package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestGetVelocityStats(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)
	keyID := "key-velocity-test"

	// Insert row 1 hour ago (counts for 24h, 7d, 30d) -> 10,000 tokens
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 10000, datetime('now', '-1 hour'))`, keyID)
	if err != nil {
		t.Fatalf("insert row 1: %v", err)
	}

	// Insert row 3 days ago (counts for 7d, 30d, but NOT 24h) -> 25,000 tokens
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 25000, datetime('now', '-3 days'))`, keyID)
	if err != nil {
		t.Fatalf("insert row 2: %v", err)
	}

	// Insert row 15 days ago (counts for 30d, but NOT 24h or 7d) -> 50,000 tokens
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 50000, datetime('now', '-15 days'))`, keyID)
	if err != nil {
		t.Fatalf("insert row 3: %v", err)
	}

	// Insert row 40 days ago (outside 30d) -> 100,000 tokens
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 100000, datetime('now', '-40 days'))`, keyID)
	if err != nil {
		t.Fatalf("insert row 4: %v", err)
	}

	now := time.Now().UTC()
	stats, err := mgr.GetVelocityStats(keyID, now)
	if err != nil {
		t.Fatalf("GetVelocityStats: %v", err)
	}

	// Tokens in past 24h = 10,000
	if stats.Tokens24h != 10000 {
		t.Errorf("expected Tokens24h = 10000, got %d", stats.Tokens24h)
	}

	// Tokens in past 7d = 10,000 + 25,000 = 35,000 -> 7d avg/day = 35,000 / 7 = 5,000
	if stats.Tokens7dAvg != 5000 {
		t.Errorf("expected Tokens7dAvg = 5000, got %d", stats.Tokens7dAvg)
	}

	// Tokens in past 30d = 10,000 + 25,000 + 50,000 = 85,000
	if stats.Tokens30d != 85000 {
		t.Errorf("expected Tokens30d = 85000, got %d", stats.Tokens30d)
	}

	// Test a brand new key (created and used only today)
	newKeyID := "key-new-today"
	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 102400, datetime('now', '-2 hours'))`, newKeyID)

	newStats, err := mgr.GetVelocityStats(newKeyID, now)
	if err != nil {
		t.Fatalf("GetVelocityStats new key: %v", err)
	}
	// For a key active for 1 day, 7d avg should be divided by 1 day (102,400), not deflated by 7
	if newStats.Tokens7dAvg != 102400 {
		t.Errorf("expected 1-day old key 7d avg to be 102400, got %d", newStats.Tokens7dAvg)
	}
}
