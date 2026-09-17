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
	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr)
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

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr)
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
