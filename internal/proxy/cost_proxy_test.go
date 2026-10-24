package proxy_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/pricing"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyWriteTimePricing(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": "chatcmpl-test",
			"object": "chat.completion",
			"created": 1234567,
			"model": "claude-3-5-sonnet",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello!"}}],
			"usage": {
				"prompt_tokens": 1000,
				"completion_tokens": 500,
				"total_tokens": 1500
			}
		}`))
	}))
	defer upstream.Close()

	prov, err := providersMgr.CreateProvider("test-upstream", upstream.URL, "key-123", "", true, true)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	_ = prov

	key, err := keyMgr.CreateKey("Test Agent", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	resolver := pricing.NewResolver(map[string]pricing.Price{
		"claude-3-5-sonnet": {
			InputPerMTok:  3.0,
			OutputPerMTok: 15.0,
		},
	}, nil)
	p.SetPricingResolver(resolver)

	reqBody := []byte(`{"model": "claude-3-5-sonnet", "messages": [{"role": "user", "content": "Hi"}]}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+key.RawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Give async log writer goroutine time to finish
	time.Sleep(100 * time.Millisecond)

	logs, total, err := trafficMgr.QueryLogs(traffic.FilterParams{})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 log row, got total=%d, len=%d", total, len(logs))
	}

	entry := logs[0]
	if entry.CostUSD == nil {
		t.Fatalf("expected cost_usd to be computed and non-nil")
	}

	expectedCost := (1000.0*3.0 + 500.0*15.0) / 1e6
	if *entry.CostUSD != expectedCost {
		t.Fatalf("expected cost_usd %v, got %v", expectedCost, *entry.CostUSD)
	}
}
