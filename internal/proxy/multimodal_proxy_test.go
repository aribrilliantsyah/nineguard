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
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyDetectsAndRecordsImages(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	// Mock upstream provider server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"choices": []interface{}{},
			"usage": map[string]int{
				"prompt_tokens":     100,
				"completion_tokens": 50,
				"total_tokens":      150,
			},
		})
	}))
	defer upstream.Close()

	_, err = providersMgr.CreateProvider("test-up", upstream.URL, "sk-upstream", "", true, true)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	clientKey, err := keyMgr.CreateKey("vision-agent", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// Payload with 2 images
	body := `{
		"model": "gpt-4o",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Analyze these diagrams"},
					{"type": "image_url", "image_url": {"url": "https://example.com/diag1.png"}},
					{"type": "image_url", "image_url": {"url": "data:image/png;base64,abc"}}
				]
			}
		]
	}`

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Wait slightly for async traffic log goroutine
	var logs []traffic.LogEntry
	for i := 0; i < 10; i++ {
		logs, _, _ = trafficMgr.QueryLogs(traffic.FilterParams{Limit: 5})
		if len(logs) > 0 {
			break
		}
	}
	if len(logs) == 0 {
		t.Fatalf("no traffic logs recorded")
	}

	entry := logs[0]
	if !entry.HasImages {
		t.Errorf("expected HasImages to be true")
	}
	if entry.ImageCount != 2 {
		t.Errorf("expected ImageCount 2, got %d", entry.ImageCount)
	}
}
