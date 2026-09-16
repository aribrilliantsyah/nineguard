package traffic

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nineguard/internal/db"
)

func setupTestDB(t *testing.T) (*Manager, func()) {
	tmpDir, err := os.MkdirTemp("", "nineguard-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	mgr := NewManager(database)
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return mgr, cleanup
}

func TestComputeLevelAndMessage(t *testing.T) {
	e1 := &LogEntry{StatusCode: 200, Model: "gpt-4o", DurationMs: 120, TotalTokens: 500}
	e1.ComputeLevelAndMessage()
	if e1.Level != "INFO" {
		t.Errorf("expected level INFO, got %s", e1.Level)
	}
	if e1.Message == "" {
		t.Errorf("expected non-empty message")
	}

	e2 := &LogEntry{StatusCode: 403, Model: "claude-3-opus", DurationMs: 15}
	e2.ComputeLevelAndMessage()
	if e2.Level != "ERROR" {
		t.Errorf("expected level ERROR for 403, got %s", e2.Level)
	}

	eWarn := &LogEntry{StatusCode: 429, Model: "gpt-4o", DurationMs: 10}
	eWarn.ComputeLevelAndMessage()
	if eWarn.Level != "WARN" {
		t.Errorf("expected level WARN for 429, got %s", eWarn.Level)
	}

	e3 := &LogEntry{StatusCode: 500, Model: "gemini-1.5-pro", DurationMs: 2000}
	e3.ComputeLevelAndMessage()
	if e3.Level != "ERROR" {
		t.Errorf("expected level ERROR, got %s", e3.Level)
	}

	e4 := &LogEntry{StatusCode: 503, Model: "mistral-large"}
	e4.ComputeLevelAndMessage()
	if e4.Level != "FATAL" {
		t.Errorf("expected level FATAL, got %s", e4.Level)
	}
}

func TestRecordAndQueryLogs(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert test entries
	_ = mgr.Record(&LogEntry{
		APIKey:      "sk-test-client-12345678",
		APIKeyName:  "dev-client",
		ProviderID:  "openai",
		Model:       "gpt-4o",
		StatusCode:  200,
		TotalTokens: 1250,
		DurationMs:  350,
		ClientIP:    "127.0.0.1",
	})
	_ = mgr.Record(&LogEntry{
		APIKey:      "sk-test-client-98765432",
		APIKeyName:  "prod-client",
		ProviderID:  "anthropic",
		Model:       "claude-3-5-sonnet",
		StatusCode:  403,
		TotalTokens: 0,
		DurationMs:  25,
		ClientIP:    "192.168.1.50",
	})
	_ = mgr.Record(&LogEntry{
		APIKey:      "sk-test-client-12345678",
		APIKeyName:  "dev-client",
		ProviderID:  "openai",
		Model:       "gpt-4o",
		StatusCode:  502,
		TotalTokens: 0,
		DurationMs:  1200,
		ClientIP:    "127.0.0.1",
	})

	// Query all
	logs, total, err := mgr.QueryLogs(FilterParams{})
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}
	if total != 3 || len(logs) != 3 {
		t.Errorf("expected 3 logs, got total %d, len %d", total, len(logs))
	}

	// Filter by Level (403 and 502 are ERROR)
	errLogs, totalErr, err := mgr.QueryLogs(FilterParams{Level: "ERROR"})
	if err != nil {
		t.Fatalf("QueryLogs level failed: %v", err)
	}
	if totalErr != 2 || len(errLogs) != 2 {
		t.Errorf("expected 2 ERROR logs (403 and 502), got %d", totalErr)
	}

	// Filter by Status: "2xx"
	status2xxLogs, total2xx, err := mgr.QueryLogs(FilterParams{Status: "2xx"})
	if err != nil || total2xx != 1 || len(status2xxLogs) != 1 || status2xxLogs[0].StatusCode != 200 {
		t.Errorf("expected 1 2xx log (200), got %d", total2xx)
	}

	// Filter by Status: "403"
	status403Logs, total403, err := mgr.QueryLogs(FilterParams{Status: "403"})
	if err != nil || total403 != 1 || len(status403Logs) != 1 || status403Logs[0].StatusCode != 403 {
		t.Errorf("expected 1 403 log, got %d", total403)
	}

	// Filter by Status: "5xx"
	status5xxLogs, total5xx, err := mgr.QueryLogs(FilterParams{Status: "5xx"})
	if err != nil || total5xx != 1 || len(status5xxLogs) != 1 || status5xxLogs[0].StatusCode != 502 {
		t.Errorf("expected 1 5xx log (502), got %d", total5xx)
	}

	// Filter by Model
	gptLogs, totalGpt, err := mgr.QueryLogs(FilterParams{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("QueryLogs model failed: %v", err)
	}
	if totalGpt != 2 || len(gptLogs) != 2 {
		t.Errorf("expected 2 gpt-4o logs, got %d", totalGpt)
	}

	// Filter by Search text
	searchLogs, totalSearch, err := mgr.QueryLogs(FilterParams{Search: "prod-client"})
	if err != nil {
		t.Fatalf("QueryLogs search failed: %v", err)
	}
	if totalSearch != 1 || len(searchLogs) != 1 {
		t.Errorf("expected 1 search result, got %d", totalSearch)
	}

	// Test GetVolume
	now := time.Now()
	vol, err := mgr.GetVolume(FilterParams{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	}, 10)
	if err != nil {
		t.Fatalf("GetVolume failed: %v", err)
	}
	if len(vol.Buckets) != 10 {
		t.Errorf("expected 10 buckets, got %d", len(vol.Buckets))
	}
	if vol.Totals["2xx"] != 1 || vol.Totals["4xx"] != 1 || vol.Totals["5xx"] != 1 {
		t.Errorf("expected 1 2xx, 1 4xx, 1 5xx, got totals: %+v", vol.Totals)
	}
}
