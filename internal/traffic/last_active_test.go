package traffic

import (
	"strings"
	"testing"
	"time"
)

func TestUsageReportLastActiveIgnoresPeriod(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")

	insertAt(t, mgr, "2026-09-15 08:00:00", "pi-dev", "9r/claude", 200, 100) // last month
	insertAt(t, mgr, "2026-10-06 09:26:40", "pi-dev", "9r/claude", 403, 0)   // today, blocked

	report, err := mgr.GetUsageReports("last_month", "", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.KeysBreakdown) != 1 {
		t.Fatalf("expected 1 key, got %d", len(report.KeysBreakdown))
	}
	k := report.KeysBreakdown[0]
	if k.TotalRequests != 1 {
		t.Errorf("period stats should only count last month: got %d requests", k.TotalRequests)
	}
	if k.LastActiveAt == nil || *k.LastActiveAt != "2026-10-06T09:26:40Z" {
		t.Errorf("key last_active_at = %v, want 2026-10-06T09:26:40Z (all-time, includes 403)", k.LastActiveAt)
	}
	if len(report.ModelsBreakdown) != 1 {
		t.Fatalf("expected 1 model, got %d", len(report.ModelsBreakdown))
	}
	if la := report.ModelsBreakdown[0].LastActiveAt; la == nil || *la != "2026-10-06T09:26:40Z" {
		t.Errorf("model last_active_at = %v", la)
	}
}

func TestUsageReportTimestampsAreRFC3339UTC(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	_ = mgr.Record(&LogEntry{APIKey: "sk-ng-test-1234567", APIKeyName: "k", Model: "m", StatusCode: 200})
	report, err := mgr.GetUsageReports("all", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	la := report.KeysBreakdown[0].LastActiveAt
	if la == nil || !strings.HasSuffix(*la, "Z") || !strings.Contains(*la, "T") {
		t.Errorf("last_active_at not RFC 3339 UTC: %v", la)
	}
}
