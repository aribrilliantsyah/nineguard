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

func TestProxyErrors(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "error_test.db")
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
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer mockUpstream.Close()

	_, _ = providersMgr.CreateProvider("test-prov", mockUpstream.URL, "prov-key", "", true, true)
	clientKey, err := keyMgr.CreateKey("test-err-key", "custom", []string{}, []string{"allowed-model"})
	if err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	// 1. Anthropic 401 Unauthorized format
	req401Anthropic := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"allowed-model"}`))
	req401Anthropic.Header.Set("x-api-key", "sk-ng-bogus")
	rec401Anthropic := httptest.NewRecorder()
	p.ServeHTTP(rec401Anthropic, req401Anthropic)

	if rec401Anthropic.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec401Anthropic.Code)
	}
	var errRespAnthropic struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec401Anthropic.Body.Bytes(), &errRespAnthropic); err != nil {
		t.Fatalf("failed to parse anthropic error JSON: %v, body=%s", err, rec401Anthropic.Body.String())
	}
	if errRespAnthropic.Type != "error" || errRespAnthropic.Error.Type != "authentication_error" {
		t.Errorf("unexpected anthropic error payload: %+v", errRespAnthropic)
	}

	// 2. OpenAI 401 Unauthorized format
	req401OpenAI := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"allowed-model"}`))
	req401OpenAI.Header.Set("Authorization", "Bearer sk-ng-bogus")
	rec401OpenAI := httptest.NewRecorder()
	p.ServeHTTP(rec401OpenAI, req401OpenAI)

	if rec401OpenAI.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec401OpenAI.Code)
	}
	var errRespOpenAI struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec401OpenAI.Body.Bytes(), &errRespOpenAI); err != nil {
		t.Fatalf("failed to parse openai error JSON: %v, body=%s", err, rec401OpenAI.Body.String())
	}
	if errRespOpenAI.Error.Type != "authentication_error" || errRespOpenAI.Error.Code != "invalid_api_key" {
		t.Errorf("unexpected openai error payload: %+v", errRespOpenAI)
	}

	// 3. Anthropic 403 Forbidden Model format
	req403Anthropic := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"disallowed-model"}`))
	req403Anthropic.Header.Set("x-api-key", clientKey.RawKey)
	rec403Anthropic := httptest.NewRecorder()
	p.ServeHTTP(rec403Anthropic, req403Anthropic)

	if rec403Anthropic.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec403Anthropic.Code, rec403Anthropic.Body.String())
	}
	var err403Anthropic struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec403Anthropic.Body.Bytes(), &err403Anthropic); err != nil {
		t.Fatalf("failed to parse anthropic 403 JSON: %v", err)
	}
	if err403Anthropic.Type != "error" || err403Anthropic.Error.Type != "permission_error" {
		t.Errorf("unexpected anthropic 403 payload: %+v", err403Anthropic)
	}

	// 4. Anthropic 429 Quota Exceeded format
	quotaKey, err := keyMgr.CreateKeyWithOptions("test-quota-key", keys.CreateKeyOptions{
		ModelAccessMode: "all",
		QuotaLimit:      100,
		QuotaPeriod:     "daily",
		QuotaMode:       "key",
	})
	if err != nil {
		t.Fatalf("failed to create quota key: %v", err)
	}
	_ = trafficMgr.Record(&traffic.LogEntry{
		APIKeyID:    quotaKey.ID,
		APIKey:      quotaKey.Key,
		TotalTokens: 200,
		StatusCode:  http.StatusOK,
	})

	req429Anthropic := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"allowed-model"}`))
	req429Anthropic.Header.Set("x-api-key", quotaKey.RawKey)
	rec429Anthropic := httptest.NewRecorder()
	p.ServeHTTP(rec429Anthropic, req429Anthropic)

	if rec429Anthropic.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec429Anthropic.Code)
	}
	var err429Anthropic struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec429Anthropic.Body.Bytes(), &err429Anthropic); err != nil {
		t.Fatalf("failed to parse anthropic 429 JSON: %v, body=%s", err, rec429Anthropic.Body.String())
	}
	if err429Anthropic.Type != "error" || err429Anthropic.Error.Type != "rate_limit_error" {
		t.Errorf("unexpected anthropic 429 payload: %+v", err429Anthropic)
	}
}
