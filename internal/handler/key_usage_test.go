package handler_test

import (
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

func TestGetKeyUsage(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	authMgr := auth.NewManager(d, false)
	modelsMgr := models.NewManager(d)
	trafficMgr := traffic.NewManager(d)
	syslogMgr := syslog.NewManager(d)
	keysMgr := keys.NewManager(d, "")
	providersMgr := providers.NewManager(d)
	revProxy, _ := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, nil)
	pricingMgr, _ := pricing.NewManager(d)

	h := handler.New(authMgr, modelsMgr, trafficMgr, syslogMgr, keysMgr, providersMgr, revProxy, "")
	h.SetPricing(pricingMgr)

	k, err := keysMgr.CreateKey("Agent Usage Key", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// 1. Success 200 with valid key and default 30d range
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/"+k.ID+"/usage?range=30d&tz=Asia/Jakarta", nil)
	req.SetPathValue("id", k.ID)
	rec := httptest.NewRecorder()

	h.GetKeyUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var sum traffic.UsageSummary
	if err := json.NewDecoder(rec.Body).Decode(&sum); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if sum.Range != "30d" || sum.Timezone != "Asia/Jakarta" {
		t.Fatalf("unexpected summary range or tz: %+v", sum)
	}

	// 2. 404 on missing key
	reqMissing := httptest.NewRequest(http.MethodGet, "/api/v1/keys/non-existent-id/usage", nil)
	reqMissing.SetPathValue("id", "non-existent-id")
	recMissing := httptest.NewRecorder()

	h.GetKeyUsage(recMissing, reqMissing)

	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", recMissing.Code)
	}

	// 3. 400 on invalid range
	reqBadRange := httptest.NewRequest(http.MethodGet, "/api/v1/keys/"+k.ID+"/usage?range=invalid", nil)
	reqBadRange.SetPathValue("id", k.ID)
	recBadRange := httptest.NewRecorder()

	h.GetKeyUsage(recBadRange, reqBadRange)

	if recBadRange.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", recBadRange.Code)
	}
}
