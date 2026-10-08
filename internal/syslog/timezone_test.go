package syslog

import (
	"testing"
	"time"
)

func TestQueryLogsPeriodUsesLoc(t *testing.T) {
	mgr, cleanup := setupTestSyslogDB(t)
	defer cleanup()

	fixed := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC) // 09:00 in Jakarta
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	defer func() { nowFunc = old }()

	for _, ts := range []string{"2026-10-05 16:59:59", "2026-10-05 17:00:00"} {
		if _, err := mgr.db.Exec(`INSERT INTO system_logs (timestamp, level, source, message) VALUES (?, 'INFO', 'test', 'm')`, ts); err != nil {
			t.Fatal(err)
		}
	}
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	_, total, err := mgr.QueryLogs(FilterParams{Period: "today", Loc: jkt})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("jakarta today: got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "yesterday", Loc: jkt})
	if total != 1 {
		t.Errorf("jakarta yesterday: got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "today"})
	if total != 0 {
		t.Errorf("utc today: got %d, want 0", total)
	}

	vol, err := mgr.GetVolume(FilterParams{Period: "today", Loc: jkt}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if vol.From != time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC).UnixNano() {
		t.Errorf("volume from = %v", time.Unix(0, vol.From).UTC())
	}
	if vol.Totals["INFO"] != 1 {
		t.Errorf("volume INFO total = %d, want 1", vol.Totals["INFO"])
	}
}
