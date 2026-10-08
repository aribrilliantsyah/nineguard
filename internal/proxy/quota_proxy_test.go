package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyQuotaExceededReturns429(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	k, err := keyMgr.CreateKeyWithOptions("quota-key", keys.CreateKeyOptions{
		QuotaLimit:  1000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// Insert past traffic exceeding 1000 tokens today
	err = trafficMgr.Record(&traffic.LogEntry{
		APIKey:      k.Key,
		APIKeyName:  k.Name,
		APIKeyID:    k.ID,
		Model:       "test-model",
		StatusCode:  200,
		TotalTokens: 1500,
		ClientIP:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("insert traffic: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", w.Code, w.Body.String())
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("expected Retry-After header to be set")
	}
	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "quota_exceeded") {
		t.Errorf("expected quota_exceeded in body, got %s", bodyStr)
	}

	// Verify error log recorded in traffic
	logs, _, err := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected blocked request to be logged in traffic, got err: %v, count: %d", err, len(logs))
	}
	if logs[0].StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected logged status 429, got %d", logs[0].StatusCode)
	}
}

func TestProxyQuotaUnderLimitProceeds(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	k, err := keyMgr.CreateKeyWithOptions("quota-key-under", keys.CreateKeyOptions{
		QuotaLimit:  1000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// Insert past traffic below 1000 tokens
	_ = trafficMgr.Record(&traffic.LogEntry{
		APIKey:      k.Key,
		APIKeyName:  k.Name,
		APIKeyID:    k.ID,
		Model:       "test-model",
		StatusCode:  200,
		TotalTokens: 500,
		ClientIP:    "127.0.0.1",
	})

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	// Since there is no upstream provider configured, expect 502 Bad Gateway (NOT 429!)
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("expected request NOT to be blocked with 429 when under quota, got 429")
	}
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502 (no provider), got %d: %s", w.Code, w.Body.String())
	}
}

