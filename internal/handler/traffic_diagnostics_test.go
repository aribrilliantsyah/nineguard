package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/traffic"
)

func TestTrafficDiagnosticsHandlerFiltering(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	tm := traffic.NewManager(d)
	h := handler.New(nil, nil, tm, nil, nil, nil, nil, "")

	// Record traffic logs with diagnostics fields
	_ = tm.Record(&traffic.LogEntry{
		Model: "m1", StatusCode: 200, ApiFormat: "chat",
		ThinkingRequested: true, ThinkingDelivered: true,
	})
	_ = tm.Record(&traffic.LogEntry{
		Model: "m2", StatusCode: 200, ApiFormat: "chat",
		ThinkingRequested: true, ThinkingDelivered: false, ModelFallback: true,
	})
	_ = tm.Record(&traffic.LogEntry{
		Model: "m3", StatusCode: 200, ApiFormat: "messages",
	})
	_ = tm.Record(&traffic.LogEntry{
		Model: "m4", StatusCode: 200, ApiFormat: "chat", UsageEstimated: true,
	})

	// 1. Filter by thinking=lost
	req := httptest.NewRequest("GET", "/api/v1/traffic?thinking=lost", nil)
	rec := httptest.NewRecorder()
	h.GetTrafficLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Logs  []traffic.LogEntry `json:"logs"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Model != "m2" {
		t.Errorf("thinking=lost filter failed, total=%d", resp.Total)
	}
	if !resp.Logs[0].ThinkingRequested || resp.Logs[0].ThinkingDelivered {
		t.Errorf("expected requested=true, delivered=false")
	}

	// 2. Filter by fallback=1
	req = httptest.NewRequest("GET", "/api/v1/traffic?fallback=1", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp.Logs = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Model != "m2" {
		t.Errorf("fallback=1 filter failed, total=%d", resp.Total)
	}

	// 3. Filter by api_format=messages
	req = httptest.NewRequest("GET", "/api/v1/traffic?api_format=messages", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp.Logs = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Model != "m3" {
		t.Errorf("api_format=messages filter failed, total=%d", resp.Total)
	}

	// 4. Filter by estimated=1
	req = httptest.NewRequest("GET", "/api/v1/traffic?estimated=1", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp.Logs = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Model != "m4" {
		t.Errorf("estimated=1 filter failed, total=%d", resp.Total)
	}
}
