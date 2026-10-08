package traffic_test

import (
	"testing"
	"time"
	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestCalcWindowBoundsUTC(t *testing.T) {
	// Fixed instant: Wednesday 2026-10-07 14:30:00 UTC
	now := time.Date(2026, 10, 7, 14, 30, 0, 0, time.UTC)

	// Daily
	start, resetAt := traffic.CalcQuotaWindow("daily", now)
	if !start.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("daily start mismatch: got %v", start)
	}
	if !resetAt.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("daily reset mismatch: got %v", resetAt)
	}

	// Weekly (Monday 00:00 UTC)
	startW, resetAtW := traffic.CalcQuotaWindow("weekly", now)
	if !startW.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("weekly start mismatch: got %v (expected Mon Oct 5)", startW)
	}
	if !resetAtW.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("weekly reset mismatch: got %v (expected Mon Oct 12)", resetAtW)
	}

	// Monthly (1st of month 00:00 UTC)
	startM, resetAtM := traffic.CalcQuotaWindow("monthly", now)
	if !startM.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("monthly start mismatch: got %v", startM)
	}
	if !resetAtM.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("monthly reset mismatch: got %v", resetAtM)
	}

	// Total (All-time)
	startT, resetAtT := traffic.CalcQuotaWindow("total", now)
	if !startT.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("total start mismatch: got %v", startT)
	}
	if resetAtT.Before(now) {
		t.Errorf("total resetAt should be in far future, got %v", resetAtT)
	}
}

func TestGetQuotaUsageQuery(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyID := "key-test-123"
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// Insert traffic row inside window
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'test-model', 200, ?, ?)`,
		keyID, 3500, "2026-10-07 10:00:00")
	if err != nil {
		t.Fatalf("insert row 1: %v", err)
	}
	// Insert traffic row outside daily window (yesterday)
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'test-model', 200, ?, ?)`,
		keyID, 5000, "2026-10-06 23:00:00")
	if err != nil {
		t.Fatalf("insert row 2: %v", err)
	}

	usage, resetAt, err := traffic.GetQuotaUsage(d, keyID, "daily", now)
	if err != nil {
		t.Fatalf("GetQuotaUsage: %v", err)
	}
	if usage != 3500 {
		t.Errorf("expected 3500 tokens in daily window, got %d", usage)
	}
	if resetAt.Before(now) {
		t.Errorf("resetAt should be future, got %v", resetAt)
	}
}
