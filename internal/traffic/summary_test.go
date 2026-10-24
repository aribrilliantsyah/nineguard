package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/pricing"
	"nineguard/internal/traffic"
)

func TestGetUsageSummary(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)
	pricingMgr, err := pricing.NewManager(d)
	if err != nil {
		t.Fatalf("NewManager pricing: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Jakarta")
	refTime := time.Date(2026, 10, 19, 14, 0, 0, 0, loc)

	// Set session gap to 30 minutes
	_ = mgr.SetSessionGapMinutes(30)

	// 1. Insert test traffic entries for key "key-agent-1"
	// Row 1: 10:00:00, 1000 prompt, 500 comp, cost $0.015
	cost1 := 0.015
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, prompt_tokens, completion_tokens, total_tokens,
			status_code, cost_usd, thinking_requested, thinking_delivered, model_fallback, usage_estimated
		) VALUES (
			datetime(?, '-7 hours'), 'sk-key1', 'key-agent-1', 'claude-3-5-sonnet', 'claude-3-5-sonnet', 1000, 500, 1500,
			200, ?, 1, 1, 0, 0
		)
	`, "2026-10-19 10:00:00", cost1)

	// Row 2: 10:15:00 (15 min after Row 1 -> SAME SESSION)
	cost2 := 0.025
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, prompt_tokens, completion_tokens, total_tokens,
			status_code, cost_usd, thinking_requested, thinking_delivered, model_fallback, usage_estimated
		) VALUES (
			datetime(?, '-7 hours'), 'sk-key1', 'key-agent-1', 'claude-3-5-sonnet', 'claude-3-5-sonnet', 2000, 500, 2500,
			200, ?, 1, 0, 1, 0
		)
	`, "2026-10-19 10:15:00", cost2)

	// Row 3: 11:30:00 (75 min after Row 2 -> NEW SESSION)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, prompt_tokens, completion_tokens, total_tokens,
			status_code, cost_usd, thinking_requested, thinking_delivered, model_fallback, usage_estimated
		) VALUES (
			datetime(?, '-7 hours'), 'sk-key1', 'key-agent-1', 'gpt-4o', 'gpt-4o', 500, 200, 700,
			200, NULL, 0, 0, 0, 0
		)
	`, "2026-10-19 11:30:00")

	// Row 4: Error request (status 500)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, prompt_tokens, completion_tokens, total_tokens,
			status_code, cost_usd, thinking_requested, thinking_delivered, model_fallback, usage_estimated
		) VALUES (
			datetime(?, '-7 hours'), 'sk-key1', 'key-agent-1', 'gpt-4o', 'gpt-4o', 0, 0, 0,
			500, NULL, 0, 0, 0, 0
		)
	`, "2026-10-19 11:35:00")

	// Row 5: Internal system row (should be EXCLUDED)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, prompt_tokens, completion_tokens, total_tokens,
			status_code, cost_usd, thinking_requested, thinking_delivered, model_fallback, usage_estimated
		) VALUES (
			datetime(?, '-7 hours'), 'sk-key1', 'key-agent-1', 'nineguard/system', 'nineguard/system', 0, 0, 0,
			200, NULL, 0, 0, 0, 0
		)
	`, "2026-10-19 11:40:00")

	// Fetch Summary
	summary, err := mgr.GetUsageSummary("key-agent-1", "30d", loc, refTime, pricingMgr)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	// 2. Validate Requests & Errors
	if summary.Requests != 3 {
		t.Fatalf("expected 3 successful requests, got %d", summary.Requests)
	}
	if summary.Errors != 1 {
		t.Fatalf("expected 1 error request, got %d", summary.Errors)
	}

	// 3. Validate Inferred Sessions:
	// Rows 1 and 2 are within 15m (1 session); Row 3 and 4 are within 5m (1 session). Total = 2 sessions!
	if summary.Sessions != 2 {
		t.Fatalf("expected 2 sessions, got %d", summary.Sessions)
	}

	// 4. Validate Tokens
	// 1500 + 2500 + 700 = 4700 total tokens
	if summary.Tokens.Total != 4700 {
		t.Fatalf("expected 4700 total tokens, got %d", summary.Tokens.Total)
	}

	// 5. Validate Peak Hour: 10:00 has 2 requests, 11:00 has 2 requests -> tie broken by earliest (10)
	if summary.PeakHour != 10 {
		t.Fatalf("expected peak hour 10, got %d", summary.PeakHour)
	}
	if summary.PeakHourFormatted != "10:00" {
		t.Fatalf("expected peak_hour_formatted '10:00', got %s", summary.PeakHourFormatted)
	}

	// 6. Validate Favorite Model: claude-3-5-sonnet has 4000 tokens, gpt-4o has 700 -> claude-3-5-sonnet
	if summary.FavoriteModel != "claude-3-5-sonnet" {
		t.Fatalf("expected favorite_model 'claude-3-5-sonnet', got %s", summary.FavoriteModel)
	}

	// 7. Validate Thinking
	if summary.Thinking.Requested != 2 || summary.Thinking.Delivered != 1 || summary.Thinking.Lost != 1 {
		t.Fatalf("expected requested 2, delivered 1, lost 1; got %+v", summary.Thinking)
	}

	// 8. Validate Fallback
	if summary.Fallbacks != 1 {
		t.Fatalf("expected 1 fallback, got %d", summary.Fallbacks)
	}

	// 9. Validate Cost
	if summary.Cost == nil {
		t.Fatalf("expected non-nil cost breakdown")
	}
	if summary.Cost.UnpricedRequests != 1 {
		t.Fatalf("expected 1 unpriced request, got %d", summary.Cost.UnpricedRequests)
	}

	// 10. Validate Invalid Range
	_, err = mgr.GetUsageSummary("key-agent-1", "invalid_range", loc, refTime, pricingMgr)
	if err == nil {
		t.Fatalf("expected error for invalid range")
	}
}

func TestUsageSummaryThinkingLostExcludesUnverifiableClaudeChat(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)
	pricingMgr, err := pricing.NewManager(d)
	if err != nil {
		t.Fatalf("NewManager pricing: %v", err)
	}

	loc := time.UTC
	refTime := time.Date(2026, 10, 19, 14, 0, 0, 0, loc)

	// 1. Claude chat, requested=1, delivered=0 (unverifiable, excluded from lost)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, api_format, prompt_tokens, completion_tokens, total_tokens,
			status_code, thinking_requested, thinking_delivered
		) VALUES (
			?, 'sk-key1', 'key-test', 'claude-3-7-sonnet', 'claude-3-7-sonnet', 'chat', 100, 50, 150,
			200, 1, 0
		)
	`, "2026-10-19 10:00:00")

	// 2. Claude chat uppercase, requested=1, delivered=0 (unverifiable case-insensitive, excluded from lost)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, api_format, prompt_tokens, completion_tokens, total_tokens,
			status_code, thinking_requested, thinking_delivered
		) VALUES (
			?, 'sk-key1', 'key-test', 'Anthropic/Claude-3-5-Sonnet', 'claude-3-5-sonnet', 'chat', 100, 50, 150,
			200, 1, 0
		)
	`, "2026-10-19 10:05:00")

	// 3. Claude messages format, requested=1, delivered=0 (real loss, included)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, api_format, prompt_tokens, completion_tokens, total_tokens,
			status_code, thinking_requested, thinking_delivered
		) VALUES (
			?, 'sk-key1', 'key-test', 'claude-3-7-sonnet', 'claude-3-7-sonnet', 'messages', 100, 50, 150,
			200, 1, 0
		)
	`, "2026-10-19 10:10:00")

	// 4. Non-Claude chat, requested=1, delivered=0 (real loss, included)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, api_format, prompt_tokens, completion_tokens, total_tokens,
			status_code, thinking_requested, thinking_delivered
		) VALUES (
			?, 'sk-key1', 'key-test', 'gpt-4o', 'gpt-4o', 'chat', 100, 50, 150,
			200, 1, 0
		)
	`, "2026-10-19 10:15:00")

	// 5. Claude chat, requested=1, delivered=1 (delivered, not lost)
	_, _ = d.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_id, model, model_id, api_format, prompt_tokens, completion_tokens, total_tokens,
			status_code, thinking_requested, thinking_delivered
		) VALUES (
			?, 'sk-key1', 'key-test', 'claude-3-7-sonnet', 'claude-3-7-sonnet', 'chat', 100, 50, 150,
			200, 1, 1
		)
	`, "2026-10-19 10:20:00")

	summary, err := mgr.GetUsageSummary("key-test", "7d", loc, refTime, pricingMgr)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	if summary.Thinking.Requested != 5 {
		t.Errorf("expected Thinking.Requested = 5, got %d", summary.Thinking.Requested)
	}
	if summary.Thinking.Delivered != 1 {
		t.Errorf("expected Thinking.Delivered = 1, got %d", summary.Thinking.Delivered)
	}
	if summary.Thinking.Lost != 2 {
		t.Errorf("expected Thinking.Lost = 2 (excluding unverifiable Claude chat), got %d", summary.Thinking.Lost)
	}
}
