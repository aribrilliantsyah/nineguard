package handler_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/traffic"
)

func TestExportUsageReportAndTrafficLogs(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tm := traffic.NewManager(database)
	h := handler.New(nil, nil, tm, nil, nil, nil, nil, "")

	now := time.Now().UTC()
	// Insert dummy traffic log with cost and diagnostics
	cost := 0.042
	reasoning := 128
	cacheRead := 256
	cacheWrite := 64
	_, err = database.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_name, model, status_code, client_ip,
			prompt_tokens, completion_tokens, total_tokens, duration_ms,
			cost_usd, served_model, model_fallback, reasoning_tokens,
			cache_read_tokens, cache_write_tokens, usage_estimated, api_format
		) VALUES (
			?, 'sk-ng-...1234', 'test-key', 'gemini-3.8-flash-high', 200, '192.168.1.1',
			1000, 200, 1200, 450,
			?, 'gemini-3.8-flash-n', 1, ?,
			?, ?, 0, 'chat'
		)
	`, now.Format("2006-01-02 15:04:05"), cost, reasoning, cacheRead, cacheWrite)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test Aggregated Usage Report Export (CSV)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/report/export?period=all", nil)
	w := httptest.NewRecorder()
	h.ExportUsageReport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "# EXECUTIVE SUMMARY") {
		t.Errorf("missing # EXECUTIVE SUMMARY in: %s", body)
	}
	if !strings.Contains(body, "# CLIENT KEYS BREAKDOWN") {
		t.Errorf("missing # CLIENT KEYS BREAKDOWN in: %s", body)
	}
	if !strings.Contains(body, "# MODELS BREAKDOWN") {
		t.Errorf("missing # MODELS BREAKDOWN in: %s", body)
	}
	if !strings.Contains(body, "# KEY TO MODEL ATTRIBUTION MATRIX") {
		t.Errorf("missing # KEY TO MODEL ATTRIBUTION MATRIX in: %s", body)
	}
	if !strings.Contains(body, "test-key") || !strings.Contains(body, "gemini-3.8-flash-high") {
		t.Errorf("missing key or model in: %s", body)
	}

	// 2. Test Raw Traffic Logs Export (CSV with new cost and diagnostics columns)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/export?period=all&format=csv", nil)
	w2 := httptest.NewRecorder()
	h.ExportTrafficLogs(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	body2 := w2.Body.String()
	headerLine := strings.Split(body2, "\n")[0]
	for _, col := range []string{
		"cost_usd", "cost_currency", "served_model", "model_fallback",
		"reasoning_tokens", "cache_read_tokens", "cache_write_tokens",
		"usage_estimated", "api_format",
	} {
		if !strings.Contains(headerLine, col) {
			t.Errorf("missing column %s in header: %s", col, headerLine)
		}
	}
	if !strings.Contains(body2, "0.042000") || !strings.Contains(body2, "gemini-3.8-flash-n") {
		t.Errorf("missing expected data row values in: %s", body2)
	}
}
