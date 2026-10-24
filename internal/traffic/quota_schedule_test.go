package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
	"nineguard/internal/traffic"
)

func insertTokens(t *testing.T, d *db.DB, keyID, modelID string, tokens int, at string) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, model_id, status_code, total_tokens, timestamp) VALUES (?, 'm', ?, 200, ?, ?)`,
		keyID, modelID, tokens, at); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// Rows are stored in UTC; the window start must be compared in UTC however the schedule's zone looks.
func TestGetQuotaUsageCountsFromScheduleWindowStart(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	// daily reset at 14:00 Asia/Jakarta = 07:00 UTC; now is 2026-10-17 08:00 UTC
	r := rule(sched("Asia/Jakarta", "14:00", 1, 1))
	now := ts("2026-10-17T08:00:00Z")

	insertTokens(t, d, "k", "", 100, "2026-10-17 06:59:59") // before the window start
	insertTokens(t, d, "k", "", 200, "2026-10-17 07:00:00") // exactly at the start
	insertTokens(t, d, "k", "", 400, "2026-10-17 07:30:00")

	got, resetAt, err := traffic.GetQuotaUsage(d, "k", "daily", r, now)
	if err != nil || got != 600 {
		t.Fatalf("usage = %d (%v), want 600", got, err)
	}
	if !resetAt.Equal(ts("2026-10-18T07:00:00Z")) {
		t.Errorf("resetAt = %s", resetAt)
	}
}

func TestGetModelQuotaUsageCountsFromScheduleWindowStart(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	r := rule(sched("Asia/Jakarta", "14:00", 1, 1))
	now := ts("2026-10-17T08:00:00Z")

	insertTokens(t, d, "k", "p/a", 100, "2026-10-17 06:59:59")
	insertTokens(t, d, "k", "p/a", 300, "2026-10-17 07:00:00")
	insertTokens(t, d, "k", "p/b", 999, "2026-10-17 07:30:00")

	got, resetAt, err := traffic.GetModelQuotaUsage(d, "k", "p/a", "daily", r, now)
	if err != nil || got != 300 {
		t.Fatalf("usage = %d (%v), want 300", got, err)
	}
	if !resetAt.Equal(ts("2026-10-18T07:00:00Z")) {
		t.Errorf("resetAt = %s", resetAt)
	}
}

// A pin makes usage count from the old window start, so an edit never changes the number.
func TestPinKeepsUsageAcrossScheduleChange(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	now := ts("2026-10-17T03:00:00Z")
	insertTokens(t, d, "k", "p/a", 4100, "2026-10-02 10:00:00")
	insertTokens(t, d, "k", "p/a", 50, "2026-09-30 10:00:00") // last month

	before := traffic.ResetRule{} // UTC default: monthly window starts Oct 1
	usageBefore, _, _ := traffic.GetQuotaUsage(d, "k", "monthly", before, now)
	modelBefore, _, _ := traffic.GetModelQuotaUsage(d, "k", "p/a", "monthly", before, now)

	newSched := sched("Asia/Jakarta", "00:00", 1, 20)
	after := traffic.ResetRule{Schedule: newSched, Pins: traffic.PinsForChange(before, newSched, now)}
	usageAfter, resetAt, _ := traffic.GetQuotaUsage(d, "k", "monthly", after, now)
	modelAfter, modelReset, _ := traffic.GetModelQuotaUsage(d, "k", "p/a", "monthly", after, now)

	if usageBefore != 4100 || usageAfter != usageBefore || modelBefore != 4100 || modelAfter != modelBefore {
		t.Fatalf("usage changed across the edit: key %d -> %d, model %d -> %d", usageBefore, usageAfter, modelBefore, modelAfter)
	}
	want := ts("2026-10-19T17:00:00Z") // Oct 20 00:00 Jakarta
	if !resetAt.Equal(want) || !modelReset.Equal(want) {
		t.Errorf("reset = %s / %s, want %s", resetAt, modelReset, want)
	}

	// after the pin ends the new window starts fresh from the new boundary
	later := want.Add(time.Hour)
	insertTokens(t, d, "k", "p/a", 10, want.Add(30*time.Minute).Format(timeutil.SQLiteLayout))
	usageLater, _, _ := traffic.GetQuotaUsage(d, "k", "monthly", after, later)
	if usageLater != 10 {
		t.Errorf("after the pin only the new window counts: got %d, want 10", usageLater)
	}
}

func TestTotalPeriodIgnoresSchedule(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	insertTokens(t, d, "k", "", 70, "2025-01-01 00:00:00")
	r := rule(sched("Asia/Jakarta", "14:00", 3, 20))
	got, _, err := traffic.GetQuotaUsage(d, "k", "total", r, ts("2026-10-17T03:00:00Z"))
	if err != nil || got != 70 {
		t.Errorf("total = %d (%v), want 70", got, err)
	}
}
