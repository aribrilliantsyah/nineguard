package traffic_test

import (
	"math"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func floatClose(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestTrafficCostAggregations(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Insert key in api_keys
	_, err = d.Exec("INSERT INTO api_keys (id, key, prefix, name) VALUES ('key-1', 'sk-1234567890', 'sk-1234', 'Agent 1')")
	if err != nil {
		t.Fatalf("failed to insert api key: %v", err)
	}

	// Insert test traffic entries
	now := time.Now().UTC()
	cost1 := 0.015
	cost2 := 0.035

	// Row 1: priced
	err = mgr.Record(&traffic.LogEntry{
		APIKey:           "sk-1234567890",
		APIKeyName:       "Agent 1",
		APIKeyID:         "key-1",
		Model:            "claude-3-5-sonnet",
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		StatusCode:       200,
		CostUSD:          &cost1,
	})
	if err != nil {
		t.Fatalf("Record row 1 failed: %v", err)
	}

	// Row 2: priced
	err = mgr.Record(&traffic.LogEntry{
		APIKey:           "sk-1234567890",
		APIKeyName:       "Agent 1",
		APIKeyID:         "key-1",
		Model:            "gpt-4o",
		PromptTokens:     2000,
		CompletionTokens: 1000,
		TotalTokens:      3000,
		StatusCode:       200,
		CostUSD:          &cost2,
	})
	if err != nil {
		t.Fatalf("Record row 2 failed: %v", err)
	}

	// Row 3: unpriced (cost_usd = nil), successful
	err = mgr.Record(&traffic.LogEntry{
		APIKey:           "sk-0987654321",
		APIKeyName:       "Agent 2",
		APIKeyID:         "key-2",
		Model:            "custom-unknown-model",
		PromptTokens:     500,
		CompletionTokens: 200,
		TotalTokens:      700,
		StatusCode:       200,
		CostUSD:          nil,
	})
	if err != nil {
		t.Fatalf("Record row 3 failed: %v", err)
	}

	// 1. Test Dashboard Stats
	stats, err := mgr.GetDashboardStats("today", "", "", time.UTC)
	if err != nil {
		t.Fatalf("GetDashboardStats failed: %v", err)
	}

	expectedCost := cost1 + cost2
	if !floatClose(stats.EstimatedCostUSD, expectedCost) {
		t.Fatalf("expected EstimatedCostUSD %v, got %v", expectedCost, stats.EstimatedCostUSD)
	}
	if stats.UnpricedRequests != 1 {
		t.Fatalf("expected 1 unpriced request, got %d", stats.UnpricedRequests)
	}

	// Verify TopKeys has cost
	if len(stats.TopKeys) == 0 {
		t.Fatalf("expected TopKeys to be populated")
	}
	var foundKey1 bool
	for _, k := range stats.TopKeys {
		if k.KeyID == "key-1" {
			foundKey1 = true
			if !floatClose(k.EstimatedCostUSD, expectedCost) {
				t.Fatalf("expected key-1 cost %v, got %v", expectedCost, k.EstimatedCostUSD)
			}
			if k.UnpricedRequests != 0 {
				t.Fatalf("expected key-1 unpriced 0, got %d", k.UnpricedRequests)
			}
		}
	}
	if !foundKey1 {
		t.Fatalf("key-1 not found in TopKeys")
	}

	// 2. Test Usage Reports
	report, err := mgr.GetUsageReports("today", "", "", time.UTC)
	if err != nil {
		t.Fatalf("GetUsageReports failed: %v", err)
	}

	if !floatClose(report.EstimatedCostUSD, expectedCost) {
		t.Fatalf("expected report EstimatedCostUSD %v, got %v", expectedCost, report.EstimatedCostUSD)
	}
	if report.UnpricedRequests != 1 {
		t.Fatalf("expected report unpriced requests 1, got %d", report.UnpricedRequests)
	}

	// Check KeysBreakdown
	var reportKey1Found bool
	for _, kb := range report.KeysBreakdown {
		if kb.KeyID == "key-1" {
			reportKey1Found = true
			if !floatClose(kb.EstimatedCostUSD, expectedCost) {
				t.Fatalf("expected kb key-1 cost %v, got %v", expectedCost, kb.EstimatedCostUSD)
			}
		}
	}
	if !reportKey1Found {
		t.Fatalf("key-1 not found in report KeysBreakdown")
	}
	_ = now
}
