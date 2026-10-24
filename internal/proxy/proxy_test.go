package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyModelPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "proxy_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// 1. Mock upstream server returning models and chat completion
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"object": "list",
				"data": [
					{"id": "gpt-4o"},
					{"id": "claude-3-5-sonnet"},
					{"id": "deepseek-r1"}
				]
			}`))
			return
		}

		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"usage": {
					"prompt_tokens": 10,
					"completion_tokens": 20,
					"total_tokens": 30
				}
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	// 2. Initialize Managers
	keysMgr := keys.NewManager(database, "")
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)
	trafficMgr := traffic.NewManager(database)

	// Create an upstream provider pointing to mock server
	_, err = providersMgr.CreateProvider("Mock Provider", mockUpstream.URL, "mock-upstream-key", "mock", true, true)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// 3. Create restricted key (only allowed "mock/gpt-4o")
	restrictedKey, err := keysMgr.CreateKey("Restricted Agent", "custom", nil, []string{"mock/gpt-4o"})
	if err != nil {
		t.Fatalf("failed to create restricted key: %v", err)
	}

	// Create unrestricted key
	unrestrictedKey, err := keysMgr.CreateKey("Unrestricted Agent", "all", nil, nil)
	if err != nil {
		t.Fatalf("failed to create unrestricted key: %v", err)
	}

	// Initialize proxy
	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// ── Test Case A: GET /v1/models with restricted key ──
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqModels.Header.Set("Authorization", "Bearer "+restrictedKey.RawKey)
	recModels := httptest.NewRecorder()
	p.ServeHTTP(recModels, reqModels)

	if recModels.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /v1/models, got %d: %s", recModels.Code, recModels.Body.String())
	}

	var modelsResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(recModels.Body).Decode(&modelsResp); err != nil {
		t.Fatalf("failed to decode models resp: %v", err)
	}
	// The restricted key only allows "mock/gpt-4o"
	if len(modelsResp.Data) != 1 {
		t.Errorf("expected 1 model returned for restricted key, got %d", len(modelsResp.Data))
	} else if modelsResp.Data[0]["id"] != "mock/gpt-4o" {
		t.Errorf("expected mock/gpt-4o, got %v", modelsResp.Data[0]["id"])
	}

	// ── Test Case B: Requesting unauthorized model with restricted key ──
	chatBodyForbidden := `{"model": "mock/claude-3-5-sonnet", "messages": [{"role":"user","content":"hello"}]}`
	reqForbidden := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBodyForbidden))
	reqForbidden.Header.Set("Authorization", "Bearer "+restrictedKey.RawKey)
	recForbidden := httptest.NewRecorder()
	p.ServeHTTP(recForbidden, reqForbidden)

	if recForbidden.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for unauthorized model, got %d: %s", recForbidden.Code, recForbidden.Body.String())
	}
	if !strings.Contains(recForbidden.Body.String(), "model_not_allowed") {
		t.Errorf("expected model_not_allowed error code, got %s", recForbidden.Body.String())
	}

	// ── Test Case C: Requesting authorized model with restricted key ──
	chatBodyAllowed := `{"model": "mock/gpt-4o", "messages": [{"role":"user","content":"hello"}]}`
	reqAllowed := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBodyAllowed))
	reqAllowed.Header.Set("Authorization", "Bearer "+restrictedKey.RawKey)
	recAllowed := httptest.NewRecorder()
	p.ServeHTTP(recAllowed, reqAllowed)

	if recAllowed.Code != http.StatusOK {
		t.Errorf("expected 200 OK for authorized model, got %d: %s", recAllowed.Code, recAllowed.Body.String())
	}

	// ── Test Case D: Requesting with unrestricted key ──
	chatBodyUnrestricted := `{"model": "mock/claude-3-5-sonnet", "messages": [{"role":"user","content":"hello"}]}`
	reqUnrestricted := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBodyUnrestricted))
	reqUnrestricted.Header.Set("Authorization", "Bearer "+unrestrictedKey.RawKey)
	recUnrestricted := httptest.NewRecorder()
	p.ServeHTTP(recUnrestricted, reqUnrestricted)

	if recUnrestricted.Code != http.StatusOK {
		t.Errorf("expected 200 OK for unrestricted key, got %d: %s", recUnrestricted.Code, recUnrestricted.Body.String())
	}
}

func TestNoProvidersSeededByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "no_seed.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	providersMgr := providers.NewManager(database)
	list, err := providersMgr.ListProviders()
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}

	// Must be empty by default!
	if len(list) != 0 {
		t.Errorf("expected 0 providers seeded by default, got %d (%v)", len(list), list)
	}
}

func TestProxyUnauthorizedTelemetry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "unauth_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	keysMgr := keys.NewManager(database, "")
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)
	trafficMgr := traffic.NewManager(database)

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("failed to create proxy: %v", err)
	}

	// 1. Request with invalid NineGuard key
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Authorization", "Bearer invalid-key-attempt")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// 2. Verify traffic log was recorded for the 401 rejection
	logs, total, err := trafficMgr.QueryLogs(traffic.FilterParams{})
	if err != nil {
		t.Fatalf("failed to query traffic logs: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 traffic log entry recorded for 401, got %d", total)
	}
	if logs[0].StatusCode != http.StatusUnauthorized {
		t.Errorf("expected logged status code 401, got %d", logs[0].StatusCode)
	}
	if logs[0].Level != "WARN" {
		t.Errorf("expected logged level WARN, got %s", logs[0].Level)
	}
}

func TestProxy_PluginPipelineExecution(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "proxy_plugin_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	var lastHeaders http.Header
	var lastReceivedBody map[string]any

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&lastReceivedBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	}))
	defer mockUpstream.Close()

	keysMgr := keys.NewManager(database, "")
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)
	trafficMgr := traffic.NewManager(database)
	pluginsMgr := plugins.NewManager(database, nil)

	_, _ = providersMgr.CreateProvider("Mock", mockUpstream.URL, "key", "mock", true, true)
	clientKey, _ := keysMgr.CreateKey("Test Agent", "all", nil, nil)

	// Turn Caveman plugin global binding ON
	_ = pluginsMgr.UpsertBinding(plugins.Binding{
		PluginID:  "caveman",
		ScopeType: plugins.ScopeGlobal,
		State:     plugins.StateOn,
	})

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, pluginsMgr)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	// 1. Send normal chat request -> Caveman should apply
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"explain quantum computing"}]}`))
	chatReq.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, chatReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify Caveman prompt injected
	msgs, _ := lastReceivedBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages received by upstream, got %d", len(msgs))
	}
	sysMsg, _ := msgs[0].(map[string]any)
	sysContent, _ := sysMsg["content"].(string)
	if !strings.Contains(sysContent, "[nineguard:caveman]") {
		t.Errorf("expected [nineguard:caveman] in upstream system message, got %s", sysContent)
	}

	// Verify applied header set upstream
	appliedHdr := lastHeaders.Get("X-NineGuard-Plugins-Applied")
	if appliedHdr != "caveman" {
		t.Errorf("expected X-NineGuard-Plugins-Applied: caveman, got %s", appliedHdr)
	}

	// 2. Send request with bypass header -> Caveman should be skipped
	bypassReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"uncompressed prompt"}]}`))
	bypassReq.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	bypassReq.Header.Set("X-NineGuard-Plugins", "off")
	recBypass := httptest.NewRecorder()
	p.ServeHTTP(recBypass, bypassReq)

	if recBypass.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recBypass.Code)
	}

	// Verify bypass header stripped from upstream request
	if lastHeaders.Get("X-NineGuard-Plugins") != "" {
		t.Errorf("X-NineGuard-Plugins should be stripped from upstream request")
	}

	// Verify Caveman was skipped
	bypassMsgs, _ := lastReceivedBody["messages"].([]any)
	if len(bypassMsgs) != 1 {
		t.Fatalf("expected 1 message (unmodified) for bypassed request, got %d", len(bypassMsgs))
	}
}

func TestDualHeaderAuth(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "dual_auth_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	keyMgr := keys.NewManager(database, "")
	trafficMgr := traffic.NewManager(database)
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-model"}]}`))
			return
		}
		if r.URL.Path == "/v1/messages" {
			_, _ = w.Write([]byte(`{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"hello anthropic"}],"model":"mock-model","usage":{"input_tokens":10,"output_tokens":5}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-123","choices":[{"message":{"role":"assistant","content":"hello openai"}}]}`))
	}))
	defer mockUpstream.Close()

	_, _ = providersMgr.CreateProvider("test-prov", mockUpstream.URL, "prov-key", "", true, true)
	clientKey, err := keyMgr.CreateKey("test-dual-auth", "all", nil, nil)
	if err != nil {
		t.Fatalf("failed to create client key: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	// 1. GET /v1/models with x-api-key
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqModels.Header.Set("x-api-key", clientKey.RawKey)
	recModels := httptest.NewRecorder()
	p.ServeHTTP(recModels, reqModels)
	if recModels.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET /v1/models with x-api-key, got %d: %s", recModels.Code, recModels.Body.String())
	}

	// 2. POST /v1/chat/completions with x-api-key
	reqChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`))
	reqChat.Header.Set("x-api-key", clientKey.RawKey)
	reqChat.Header.Set("Content-Type", "application/json")
	recChat := httptest.NewRecorder()
	p.ServeHTTP(recChat, reqChat)
	if recChat.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for POST /v1/chat/completions with x-api-key, got %d: %s", recChat.Code, recChat.Body.String())
	}

	// 3. POST /v1/messages with x-api-key
	reqMessages := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`))
	reqMessages.Header.Set("x-api-key", clientKey.RawKey)
	reqMessages.Header.Set("Content-Type", "application/json")
	recMessages := httptest.NewRecorder()
	p.ServeHTTP(recMessages, reqMessages)
	if recMessages.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for POST /v1/messages with x-api-key, got %d: %s", recMessages.Code, recMessages.Body.String())
	}

	// 4. Invalid x-api-key -> 401
	reqInvalid := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqInvalid.Header.Set("x-api-key", "sk-ng-invalid")
	recInvalid := httptest.NewRecorder()
	p.ServeHTTP(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid x-api-key, got %d", recInvalid.Code)
	}

	// 5. Empty auth -> 401
	reqEmpty := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recEmpty := httptest.NewRecorder()
	p.ServeHTTP(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for empty auth, got %d", recEmpty.Code)
	}
}


