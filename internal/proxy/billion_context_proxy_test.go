package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/plugins/builtin"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxy_BillionContext_ForwardingAndHost(t *testing.T) {
	builtin.ResetBillionContextHealthCache()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "proxy_bili_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	var mu sync.Mutex
	var lastBiliReceivedPath string
	var lastBiliReceivedHost string
	var upstreamReceivedPath string

	// 1. Mock Upstream Provider
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upstreamReceivedPath = r.URL.Path
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "chatcmpl-bili",
				"object": "chat.completion",
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "hello from upstream"}},
				},
				"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "msg-bili",
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": "hello from upstream claude"}},
				"usage":   map[string]any{"input_tokens": 5, "output_tokens": 5},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	// 2. Mock Billion Context Server
	mockBili := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__bili/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/bili/") {
			mu.Lock()
			lastBiliReceivedPath = r.URL.RequestURI()
			lastBiliReceivedHost = r.Host
			mu.Unlock()

			// Forward to actual target embedded in path: /bili/<targetURL>
			rawTarget := strings.TrimPrefix(r.URL.RequestURI(), "/bili/")
			targetReq, err := http.NewRequestWithContext(r.Context(), r.Method, rawTarget, r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for k, vv := range r.Header {
				for _, v := range vv {
					targetReq.Header.Add(k, v)
				}
			}
			resp, err := http.DefaultClient.Do(targetReq)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write([]byte(`{"proxied_via_bili":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockBili.Close()

	parsedBiliURL, _ := url.Parse(mockBili.URL)
	expectedBiliHost := parsedBiliURL.Host

	// 3. Initialize managers and configure billion-context plugin
	keysMgr := keys.NewManager(database, "")
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)
	trafficMgr := traffic.NewManager(database)
	pluginsMgr := plugins.NewManager(database, mockBili.Client())

	_, _ = providersMgr.CreateProvider("Mock Provider", mockUpstream.URL, "mock-upstream-key", "mock", true, true)
	clientKey, _ := keysMgr.CreateKey("Test Agent", "all", nil, nil)

	// Update billion-context default settings URL to mockBili.URL and turn global binding ON
	biliSettingsJSON := `{"url":"` + mockBili.URL + `"}`
	_, _ = database.Exec("UPDATE plugins SET default_settings = ? WHERE id = 'billion-context'", biliSettingsJSON)
	_ = pluginsMgr.ReloadCache()
	_ = pluginsMgr.UpsertBinding(plugins.Binding{
		PluginID:  "billion-context",
		ScopeType: plugins.ScopeGlobal,
		State:     plugins.StateOn,
		Settings:  biliSettingsJSON,
	})

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, pluginsMgr)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	// ── Case A: /v1/chat/completions ──
	mu.Lock()
	lastBiliReceivedPath = ""
	lastBiliReceivedHost = ""
	upstreamReceivedPath = ""
	mu.Unlock()

	chatBody := `{"model":"mock/gpt-4o","messages":[{"role":"user","content":"ping"}]}`
	reqChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	reqChat.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	recChat := httptest.NewRecorder()
	p.ServeHTTP(recChat, reqChat)

	if recChat.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /v1/chat/completions, got %d: %s", recChat.Code, recChat.Body.String())
	}

	mu.Lock()
	biliPath := lastBiliReceivedPath
	biliHost := lastBiliReceivedHost
	upPath := upstreamReceivedPath
	mu.Unlock()

	if !strings.HasPrefix(biliPath, "/bili/"+mockUpstream.URL) {
		t.Fatalf("expected bili request path to wrap upstream URL, got %s", biliPath)
	}
	if biliHost != expectedBiliHost {
		t.Fatalf("expected outReq.Host to be bili host %s, got %s", expectedBiliHost, biliHost)
	}
	if !strings.HasSuffix(upPath, "/chat/completions") {
		t.Fatalf("expected upstream to receive chat completions path, got %s", upPath)
	}

	// ── Case B: /v1/messages ──
	mu.Lock()
	lastBiliReceivedPath = ""
	lastBiliReceivedHost = ""
	upstreamReceivedPath = ""
	mu.Unlock()

	msgBody := `{"model":"mock/claude-3-7-sonnet","messages":[{"role":"user","content":"ping"}]}`
	reqMsg := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(msgBody))
	reqMsg.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	recMsg := httptest.NewRecorder()
	p.ServeHTTP(recMsg, reqMsg)

	if recMsg.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /v1/messages, got %d: %s", recMsg.Code, recMsg.Body.String())
	}

	mu.Lock()
	biliPath = lastBiliReceivedPath
	biliHost = lastBiliReceivedHost
	upPath = upstreamReceivedPath
	mu.Unlock()

	if !strings.HasPrefix(biliPath, "/bili/"+mockUpstream.URL) {
		t.Fatalf("expected bili request path to wrap upstream URL for /v1/messages, got %s", biliPath)
	}
	if biliHost != expectedBiliHost {
		t.Fatalf("expected outReq.Host to be bili host %s, got %s", expectedBiliHost, biliHost)
	}
	if !strings.HasSuffix(upPath, "/messages") {
		t.Fatalf("expected upstream to receive messages path, got %s", upPath)
	}
}
