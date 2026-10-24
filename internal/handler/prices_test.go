package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/pricing"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/syslog"
	"nineguard/internal/traffic"
)

func setupTestHandlerWithPricing(t *testing.T) (*handler.Handler, *pricing.Manager, *db.DB) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	authMgr := auth.NewManager(d, false)
	modelsMgr := models.NewManager(d)
	trafficMgr := traffic.NewManager(d)
	syslogMgr := syslog.NewManager(d)
	keysMgr := keys.NewManager(d, "")
	providersMgr := providers.NewManager(d)
	revProxy, _ := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, nil)

	pricingMgr, err := pricing.NewManager(d)
	if err != nil {
		t.Fatalf("NewManager pricing: %v", err)
	}
	revProxy.SetPricingResolver(pricingMgr.Resolver())

	h := handler.New(authMgr, modelsMgr, trafficMgr, syslogMgr, keysMgr, providersMgr, revProxy, "")
	h.SetPricing(pricingMgr)

	return h, pricingMgr, d
}

func TestPricingEndpoints(t *testing.T) {
	h, pricingMgr, d := setupTestHandlerWithPricing(t)
	defer d.Close()

	// 1. Add model to system
	_, err := d.Exec("INSERT INTO models (id, name, enabled) VALUES ('claude-3-5-sonnet', 'Claude 3.5 Sonnet', 1)")
	if err != nil {
		t.Fatalf("insert model: %v", err)
	}

	// GET /api/v1/prices
	req := httptest.NewRequest("GET", "/api/v1/prices", nil)
	rec := httptest.NewRecorder()
	h.GetPrices(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var prices []pricing.ModelWithPrice
	if err := json.NewDecoder(rec.Body).Decode(&prices); err != nil {
		t.Fatalf("decode prices: %v", err)
	}
	if len(prices) == 0 {
		t.Fatalf("expected non-empty prices list")
	}

	// 2. PUT /api/v1/prices/overrides/{model_id...}
	overrideBody := []byte(`{
		"input_per_mtok": 5.0,
		"output_per_mtok": 20.0
	}`)
	putReq := httptest.NewRequest("PUT", "/api/v1/prices/overrides/claude-3-5-sonnet", bytes.NewReader(overrideBody))
	putReq.SetPathValue("model_id", "claude-3-5-sonnet")
	putRec := httptest.NewRecorder()
	h.SetPriceOverride(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on put override, got %d: %s", putRec.Code, putRec.Body.String())
	}

	p, src, found := pricingMgr.Resolver().PriceFor("claude-3-5-sonnet")
	if !found || src != "override" || p.InputPerMTok != 5.0 {
		t.Fatalf("expected override price 5.0, got found=%v, src=%s, p=%+v", found, src, p)
	}

	// 3. DELETE /api/v1/prices/overrides/{model_id...}
	delReq := httptest.NewRequest("DELETE", "/api/v1/prices/overrides/claude-3-5-sonnet", nil)
	delReq.SetPathValue("model_id", "claude-3-5-sonnet")
	delRec := httptest.NewRecorder()
	h.DeletePriceOverride(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete override, got %d", delRec.Code)
	}

	p, src, found = pricingMgr.Resolver().PriceFor("claude-3-5-sonnet")
	if !found || src != "list" {
		t.Fatalf("expected list price after delete override, got found=%v, src=%s", found, src)
	}

	// 4. GET & POST /api/v1/settings/cost
	getSetReq := httptest.NewRequest("GET", "/api/v1/settings/cost", nil)
	getSetRec := httptest.NewRecorder()
	h.GetCostSettings(getSetRec, getSetReq)

	if getSetRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get settings, got %d", getSetRec.Code)
	}

	postSetBody := []byte(`{
		"display_currency": "IDR",
		"fx_rate_manual": "17000",
		"cost_visible_to_keys": false
	}`)
	postSetReq := httptest.NewRequest("POST", "/api/v1/settings/cost", bytes.NewReader(postSetBody))
	postSetRec := httptest.NewRecorder()
	h.UpdateCostSettings(postSetRec, postSetReq)

	if postSetRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on post settings, got %d", postSetRec.Code)
	}

	set, _ := pricingMgr.GetCostSettings()
	if set.DisplayCurrency != "IDR" || set.FXRateManual != "17000" || set.CostVisibleToKeys != false {
		t.Fatalf("expected updated settings, got %+v", set)
	}

	// 5. POST /api/v1/costs/reprice
	repriceBody := []byte(`{}`)
	repriceReq := httptest.NewRequest("POST", "/api/v1/costs/reprice", bytes.NewReader(repriceBody))
	repriceRec := httptest.NewRecorder()
	h.RepriceCosts(repriceRec, repriceReq)

	if repriceRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on reprice, got %d", repriceRec.Code)
	}
}
