package db_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
	_ "modernc.org/sqlite"
)

func TestUsageDiagnosticsMigration(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	// Verify columns exist in traffic_logs
	expectedCols := []string{
		"api_format",
		"cache_read_tokens",
		"cache_write_tokens",
		"reasoning_tokens",
		"thinking_requested",
		"thinking_delivered",
		"served_model",
		"model_fallback",
		"usage_estimated",
	}

	for _, col := range expectedCols {
		var count int
		query := `SELECT COUNT(*) FROM pragma_table_info('traffic_logs') WHERE name = ?`
		if err := d.QueryRow(query, col).Scan(&count); err != nil {
			t.Fatalf("failed to query table info for %s: %v", col, err)
		}
		if count != 1 {
			t.Errorf("expected column %q to exist in traffic_logs, count = %d", col, count)
		}
	}

	// Verify stream_usage_injection default setting exists
	var streamUsageInj string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'stream_usage_injection'").Scan(&streamUsageInj)
	if err != nil {
		t.Fatalf("expected stream_usage_injection in settings, got: %v", err)
	}
	if streamUsageInj != "true" {
		t.Fatalf("expected stream_usage_injection to be 'true', got %q", streamUsageInj)
	}

	// Verify idempotent re-run of migration
	// Calling InitDB on existing DB handle should not fail
	tm := traffic.NewManager(d)
	if !tm.IsStreamUsageInjectionEnabled() {
		t.Errorf("expected IsStreamUsageInjectionEnabled to be true by default")
	}
	if err := tm.SetStreamUsageInjectionEnabled(false); err != nil {
		t.Fatalf("SetStreamUsageInjectionEnabled(false) failed: %v", err)
	}
	if tm.IsStreamUsageInjectionEnabled() {
		t.Errorf("expected IsStreamUsageInjectionEnabled to be false after update")
	}
}
