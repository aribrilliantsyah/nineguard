package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/pricing"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestFormatUsageMarkdown(t *testing.T) {
	summary := &traffic.UsageSummary{
		Range:             "30d",
		Timezone:          "Asia/Jakarta",
		Requests:          1204,
		Errors:            31,
		ErrorRatePct:      2.5,
		Sessions:          43,
		ActiveDays:        12,
		PeakHourFormatted: "00:00",
		FavoriteModel:     "claude-opus-4.5",
		Tokens: traffic.TokensBreakdown{
			Total:      4600000000,
			Prompt:     1200000000,
			Completion: 7000000,
		},
		CacheHitPct: 91.0,
		Thinking: traffic.ThinkingBreakdown{
			Requested: 812,
			Delivered: 790,
			Lost:      22,
		},
		Fallbacks: 3,
		Cost: &traffic.CostBreakdown{
			USD:              123.45,
			Display:          2225900,
			DisplayFormatted: "Rp 2.225.900",
			Currency:         "IDR",
		},
		Models: []traffic.ModelUsagePoint{
			{
				ModelID:    "claude-opus-4.5",
				Prompt:     428100,
				Completion: 3700000,
				PctTokens:  69.9,
			},
		},
		Comparison: &traffic.ComparisonResult{
			Book:     "Animal Farm",
			Multiple: 117869.2,
			Text:     "You've used ~117,869× more tokens than Animal Farm.",
		},
	}

	// 1. With cost visible
	md := proxy.FormatUsageMarkdown(summary, "30d", true)
	if !strings.Contains(md, "NineGuard Usage — last 30 days") {
		t.Fatalf("expected title with last 30 days, got:\n%s", md)
	}
	if !strings.Contains(md, "Rp 2.225.900") {
		t.Fatalf("expected cost in markdown, got:\n%s", md)
	}
	if strings.Contains(md, "Animal Farm") {
		t.Fatalf("expected book flavor message to be omitted, got:\n%s", md)
	}

	// 2. With cost hidden
	mdHidden := proxy.FormatUsageMarkdown(summary, "30d", false)
	if strings.Contains(mdHidden, "Rp 2.225.900") || strings.Contains(mdHidden, "Est. cost") {
		t.Fatalf("expected cost to be hidden, got:\n%s", mdHidden)
	}

	// 3. Unknown range argument note
	mdUnknown := proxy.FormatUsageMarkdown(summary, "invalid_arg", true)
	if !strings.Contains(mdUnknown, `Unknown range "invalid_arg", showing 30d. Use /usage 7d|30d|all.`) {
		t.Fatalf("expected unknown range warning line, got:\n%s", mdUnknown)
	}
}

func TestProxyGetUsageEndpoint(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)
	pricingMgr, _ := pricing.NewManager(d)

	key, err := keyMgr.CreateKey("Test Key", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	p.SetPricing(pricingMgr)

	// 1. Valid request to GET /v1/usage
	req := httptest.NewRequest(http.MethodGet, "/v1/usage?range=7d", nil)
	req.Header.Set("Authorization", "Bearer "+key.RawKey)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var sum traffic.UsageSummary
	if err := json.NewDecoder(rec.Body).Decode(&sum); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
	if sum.Range != "7d" {
		t.Fatalf("expected range 7d, got %s", sum.Range)
	}

	// 2. Invalid range returns 400
	reqBad := httptest.NewRequest(http.MethodGet, "/v1/usage?range=bad_range", nil)
	reqBad.Header.Set("Authorization", "Bearer "+key.RawKey)
	recBad := httptest.NewRecorder()

	p.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", recBad.Code)
	}

	// 3. Unauthorized request returns 401
	reqUnauth := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	recUnauth := httptest.NewRecorder()

	p.ServeHTTP(recUnauth, reqUnauth)

	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", recUnauth.Code)
	}
}
