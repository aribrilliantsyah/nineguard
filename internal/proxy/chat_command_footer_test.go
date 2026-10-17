package proxy_test

import (
	"strings"
	"testing"
	"time"

	"nineguard/internal/proxy"
	"nineguard/internal/upstream"
)

func quotaRespWithPool(syncInterval string) *upstream.UnifiedQuotaResponse {
	now := time.Now().Add(-2 * time.Minute)
	return &upstream.UnifiedQuotaResponse{
		UpstreamPools: []upstream.MergedPoolQuota{{
			Provider: "9router",
			Family:   "Gemini",
			Buckets: []upstream.QuotaBucket{
				{Name: "Gemini (5h)", Used: 130, Total: 1000, Remaining: 870, RemainingPercentage: 87},
			},
		}},
		LastUpdated:  &now,
		SyncInterval: syncInterval,
	}
}

func TestFormatQuotaMarkdown_FooterUsesConfiguredInterval(t *testing.T) {
	md := proxy.FormatQuotaMarkdown(quotaRespWithPool("15m"), "")
	if !strings.Contains(md, "Auto-refreshes every 15m") {
		t.Fatalf("footer should show configured interval, got:\n%s", md)
	}
	if strings.Contains(md, "every 5m") {
		t.Fatalf("footer must not show hardcoded 5m, got:\n%s", md)
	}
}

func TestFormatQuotaMarkdown_FooterFallsBackToDefault(t *testing.T) {
	md := proxy.FormatQuotaMarkdown(quotaRespWithPool(""), "")
	if !strings.Contains(md, "Auto-refreshes every 10m") {
		t.Fatalf("footer should fall back to 10m default, got:\n%s", md)
	}
}

func TestFormatQuotaMarkdown_SubProviderFormattingAndFiltering(t *testing.T) {
	resp := &upstream.UnifiedQuotaResponse{
		UpstreamPools: []upstream.MergedPoolQuota{
			{
				Provider:    "nineguard kantor",
				SubProvider: "antigravity",
				Family:      "nineguard kantor · Antigravity",
				Buckets: []upstream.QuotaBucket{
					{Name: "Claude & GPT (5h)", Used: 450, Total: 2000, Remaining: 1550, RemainingPercentage: 77},
					{Name: "Gemini (5h)", Used: 800, Total: 2000, Remaining: 1200, RemainingPercentage: 60},
				},
			},
			{
				Provider:    "nineguard kantor",
				SubProvider: "copilot",
				Family:      "nineguard kantor · Copilot",
				Buckets: []upstream.QuotaBucket{
					{Name: "5h Session", Used: 5, Total: 100, Remaining: 95, RemainingPercentage: 95},
				},
			},
		},
	}

	// 1. Full report prints composite Family
	allMd := proxy.FormatQuotaMarkdown(resp, "")
	if !strings.Contains(allMd, "nineguard kantor · Antigravity") {
		t.Fatalf("expected Antigravity composite family in table, got:\n%s", allMd)
	}
	if !strings.Contains(allMd, "nineguard kantor · Copilot") {
		t.Fatalf("expected Copilot composite family in table, got:\n%s", allMd)
	}

	// 2. Filter by sub-provider "copilot"
	copilotMd := proxy.FormatQuotaMarkdown(resp, "copilot")
	if strings.Contains(copilotMd, "Antigravity") {
		t.Fatalf("expected Copilot filter to exclude Antigravity, got:\n%s", copilotMd)
	}
	if !strings.Contains(copilotMd, "nineguard kantor · Copilot") || !strings.Contains(copilotMd, "5h Session") {
		t.Fatalf("expected Copilot bucket in filtered markdown, got:\n%s", copilotMd)
	}

	// 3. Filter by sub-provider "antigravity"
	antiMd := proxy.FormatQuotaMarkdown(resp, "antigravity")
	if strings.Contains(antiMd, "Copilot") {
		t.Fatalf("expected Antigravity filter to exclude Copilot, got:\n%s", antiMd)
	}
	if !strings.Contains(antiMd, "nineguard kantor · Antigravity") {
		t.Fatalf("expected Antigravity pool in filtered markdown, got:\n%s", antiMd)
	}
}
