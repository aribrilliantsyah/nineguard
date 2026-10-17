package upstream_test

import (
	"testing"
	"time"

	"nineguard/internal/upstream"
)

func TestMergeAccountPools_MathAndEarliestReset(t *testing.T) {
	now := time.Now()
	t1 := now.Add(22 * time.Hour)
	t2 := now.Add(23 * time.Hour)

	// Simulate user screenshot with 2 connections
	conns := []upstream.ConnectionQuota{
		{
			ConnectionID: "acc1",
			AccountLabel: "personal@gmail.com",
			Status:       "ok",
			Buckets: []upstream.QuotaBucket{
				{
					Name:                "Gemini (Weekly)",
					Used:                778,
					Total:               1000,
					Remaining:           222,
					RemainingPercentage: 22,
					ResetAt:             &t1,
				},
				{
					Name:                "Claude & GPT (5h)",
					Used:                0,
					Total:               1000,
					Remaining:           1000,
					RemainingPercentage: 100,
					ResetAt:             &t2,
				},
			},
		},
		{
			ConnectionID: "acc2",
			AccountLabel: "work@corp.com",
			Status:       "ok",
			Buckets: []upstream.QuotaBucket{
				{
					Name:                "Gemini (Weekly)",
					Used:                777,
					Total:               1000,
					Remaining:           223,
					RemainingPercentage: 22,
					ResetAt:             &t2,
				},
				{
					Name:                "Claude & GPT (5h)",
					Used:                0,
					Total:               1000,
					Remaining:           1000,
					RemainingPercentage: 100,
					ResetAt:             &t1,
				},
			},
		},
	}

	pools := upstream.MergeAccountPools("9router", conns)
	if len(pools) != 1 {
		t.Fatalf("expected 1 merged pool, got %d", len(pools))
	}
	merged := pools[0]

	if merged.Provider != "9router" {
		t.Fatalf("expected provider 9router, got %s", merged.Provider)
	}
	if !merged.Warning {
		t.Fatalf("expected merged pool to flag warning due to Gemini 22%%")
	}

	if len(merged.Buckets) != 2 {
		t.Fatalf("expected 2 merged buckets, got %d", len(merged.Buckets))
	}

	var geminiWeekly, claude5h *upstream.QuotaBucket
	for i := range merged.Buckets {
		if merged.Buckets[i].Name == "Gemini (Weekly)" {
			geminiWeekly = &merged.Buckets[i]
		} else if merged.Buckets[i].Name == "Claude & GPT (5h)" {
			claude5h = &merged.Buckets[i]
		}
	}

	if geminiWeekly == nil {
		t.Fatalf("missing Gemini (Weekly) bucket")
	}
	if geminiWeekly.Used != 1555 || geminiWeekly.Total != 2000 {
		t.Fatalf("expected 1555/2000, got %d/%d", geminiWeekly.Used, geminiWeekly.Total)
	}
	if geminiWeekly.Remaining != 445 {
		t.Fatalf("expected remaining 445, got %d", geminiWeekly.Remaining)
	}
	if geminiWeekly.RemainingPercentage != 22 {
		t.Fatalf("expected 22%%, got %d%%", geminiWeekly.RemainingPercentage)
	}
	if !geminiWeekly.Warning {
		t.Fatalf("expected warning on Gemini Weekly")
	}
	if geminiWeekly.ResetAt == nil || !geminiWeekly.ResetAt.Equal(t1) {
		t.Fatalf("expected earliest reset %v, got %v", t1, geminiWeekly.ResetAt)
	}

	if claude5h == nil {
		t.Fatalf("missing Claude & GPT (5h) bucket")
	}
	if claude5h.Used != 0 || claude5h.Total != 2000 {
		t.Fatalf("expected 0/2000, got %d/%d", claude5h.Used, claude5h.Total)
	}
	if claude5h.RemainingPercentage != 100 || claude5h.Warning {
		t.Fatalf("expected 100%% without warning, got %d%%", claude5h.RemainingPercentage)
	}
}

func TestFilterPoolsByModelAccess(t *testing.T) {
	pools := []upstream.MergedPoolQuota{
		{
			Provider:      "9router",
			Family:        "Claude",
			AllowedModels: []string{"claude-3-7-sonnet", "claude-3-5-sonnet"},
		},
		{
			Provider:      "9router",
			Family:        "OpenAI",
			AllowedModels: []string{"gpt-4o", "gpt-4o-mini"},
		},
	}

	// Key allowed for Claude only
	filtered := upstream.FilterPoolsByModelAccess(pools, []string{"claude-3-7-sonnet"})
	if len(filtered) != 1 || filtered[0].Family != "Claude" {
		t.Fatalf("expected only Claude pool, got: %+v", filtered)
	}

	// Wildcard allows all
	wildcard := upstream.FilterPoolsByModelAccess(pools, []string{"*"})
	if len(wildcard) != 2 {
		t.Fatalf("expected all pools on wildcard, got %d", len(wildcard))
	}
}

func TestMergeAccountPools_SubProviderGrouping(t *testing.T) {
	conns := []upstream.ConnectionQuota{
		{
			ConnectionID: "c1",
			Provider:     "antigravity",
			Buckets: []upstream.QuotaBucket{
				{Name: "Claude & GPT (5h)", Used: 10, Total: 1000},
			},
		},
		{
			ConnectionID: "c2",
			Provider:     "antigravity",
			Buckets: []upstream.QuotaBucket{
				{Name: "Gemini (5h)", Used: 20, Total: 1000},
			},
		},
		{
			ConnectionID: "c3",
			Provider:     "copilot",
			Buckets: []upstream.QuotaBucket{
				{Name: "5h Session", Used: 5, Total: 100},
			},
		},
	}

	pools := upstream.MergeAccountPools("9router DAK", conns)
	if len(pools) != 2 {
		t.Fatalf("expected 2 distinct pools, got %d: %+v", len(pools), pools)
	}

	p1 := pools[0]
	if p1.Provider != "9router DAK" || p1.SubProvider != "antigravity" || p1.Family != "9router DAK · Antigravity" {
		t.Fatalf("unexpected pool 1: %+v", p1)
	}
	if len(p1.Buckets) != 2 {
		t.Fatalf("expected 2 buckets in Antigravity pool, got %d", len(p1.Buckets))
	}

	p2 := pools[1]
	if p2.Provider != "9router DAK" || p2.SubProvider != "copilot" || p2.Family != "9router DAK · Copilot" {
		t.Fatalf("unexpected pool 2: %+v", p2)
	}
	if len(p2.Buckets) != 1 || p2.Buckets[0].Name != "5h Session" {
		t.Fatalf("expected 1 bucket '5h Session' in Copilot pool, got %+v", p2.Buckets)
	}
}
