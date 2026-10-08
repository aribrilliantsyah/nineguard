package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyPayloadCapture(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	// Mock upstream
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-payload",
			"choices": []interface{}{},
		})
	}))
	defer upstream.Close()

	_, _ = providersMgr.CreateProvider("test-up", upstream.URL, "sk-test", "", true, true)
	clientKey, _ := keyMgr.CreateKey("payload-agent", "all", nil, nil)
	p, _ := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)

	// Case 1: record_payloads = disabled (default)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	time.Sleep(50 * time.Millisecond) // wait for async logging

	logs, _, _ := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 1})
	if len(logs) == 0 {
		t.Fatalf("no log found")
	}
	_, err = trafficMgr.GetPayload(logs[0].ID)
	if err == nil {
		t.Errorf("expected no payload stored when setting is disabled")
	}

	// Case 2: record_payloads = all
	_ = trafficMgr.SetRecordPayloadsSetting("all")

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	time.Sleep(50 * time.Millisecond)

	logs2, _, _ := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 1})
	if len(logs2) == 0 {
		t.Fatalf("no log found")
	}
	payload, err := trafficMgr.GetPayload(logs2[0].ID)
	if err != nil {
		t.Fatalf("expected payload stored when setting is all, got err: %v", err)
	}
	if !strings.Contains(payload.RequestBody, "ping") {
		t.Errorf("expected request body to contain 'ping', got %s", payload.RequestBody)
	}
	if !strings.Contains(payload.ResponseBody, "chatcmpl-payload") {
		t.Errorf("expected response body to contain upstream json, got %s", payload.ResponseBody)
	}
}
