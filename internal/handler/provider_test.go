package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/providers"
)

func TestHandlerProviderDefaultFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_handler_provider.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	authMgr := auth.NewManager(database, true)
	provMgr := providers.NewManager(database)

	h := handler.New(authMgr, nil, nil, nil, nil, provMgr, nil, "http://localhost:8080")

	// 1. Create first provider
	p1Body := map[string]interface{}{
		"name":       "Ollama Local",
		"route":      "http://localhost:11434",
		"api_key":    "",
		"prefix":     "ollama",
		"is_default": false,
		"is_active":  true,
	}
	bodyBytes, _ := json.Marshal(p1Body)
	req := httptest.NewRequest("POST", "/api/v1/providers", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("CreateProvider returned %d: %s", w.Code, w.Body.String())
	}
	var createdP1 providers.Provider
	_ = json.NewDecoder(w.Body).Decode(&createdP1)
	if !createdP1.IsDefault {
		t.Errorf("expected first provider to be default")
	}

	// 2. Create second provider with is_default = true
	p2Body := map[string]interface{}{
		"name":       "OpenAI Official",
		"route":      "https://api.openai.com",
		"api_key":    "sk-test",
		"prefix":     "openai",
		"is_default": true,
		"is_active":  true,
	}
	bodyBytes, _ = json.Marshal(p2Body)
	req = httptest.NewRequest("POST", "/api/v1/providers", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("CreateProvider p2 returned %d: %s", w.Code, w.Body.String())
	}
	var createdP2 providers.Provider
	_ = json.NewDecoder(w.Body).Decode(&createdP2)
	if !createdP2.IsDefault {
		t.Errorf("expected p2 to be default")
	}

	// Verify only p2 is default via ListProviders
	req = httptest.NewRequest("GET", "/api/v1/providers", nil)
	w = httptest.NewRecorder()
	h.ListProviders(w, req)
	var listResp struct {
		Providers []providers.Provider `json:"providers"`
	}
	_ = json.NewDecoder(w.Body).Decode(&listResp)

	defaultsCount := 0
	var currentDefault *providers.Provider
	for _, p := range listResp.Providers {
		if p.IsDefault {
			defaultsCount++
			pCopy := p
			currentDefault = &pCopy
		}
	}
	if defaultsCount != 1 || currentDefault == nil || currentDefault.ID != createdP2.ID {
		t.Fatalf("expected exactly 1 default provider (p2), got %d defaults", defaultsCount)
	}

	// 3. Update provider p1 to be default: PUT /api/v1/providers/{id}
	p1Update := map[string]interface{}{
		"name":       "Ollama Local Renamed",
		"route":      "http://localhost:11434",
		"api_key":    "",
		"prefix":     "ollama",
		"is_default": true,
		"is_active":  true,
	}
	bodyBytes, _ = json.Marshal(p1Update)
	req = httptest.NewRequest("PUT", "/api/v1/providers/"+createdP1.ID, bytes.NewReader(bodyBytes))
	req.SetPathValue("id", createdP1.ID)
	w = httptest.NewRecorder()
	h.UpdateProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProvider p1 returned %d: %s", w.Code, w.Body.String())
	}
	var updatedP1 providers.Provider
	_ = json.NewDecoder(w.Body).Decode(&updatedP1)
	if !updatedP1.IsDefault {
		t.Errorf("expected updated p1 to be default")
	}

	// Verify p2 is no longer default
	p2Check, _ := provMgr.GetProvider(createdP2.ID)
	if p2Check.IsDefault {
		t.Errorf("expected p2 to no longer be default")
	}

	// 4. Test SetDefaultProvider endpoint: POST /api/v1/providers/{id}/default
	req = httptest.NewRequest("POST", "/api/v1/providers/"+createdP2.ID+"/default", nil)
	req.SetPathValue("id", createdP2.ID)
	w = httptest.NewRecorder()
	h.SetDefaultProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("SetDefaultProvider returned %d: %s", w.Code, w.Body.String())
	}

	p2Check, _ = provMgr.GetProvider(createdP2.ID)
	if !p2Check.IsDefault {
		t.Errorf("expected p2 to be default after SetDefaultProvider")
	}
	p1Check, _ := provMgr.GetProvider(createdP1.ID)
	if p1Check.IsDefault {
		t.Errorf("expected p1 to no longer be default after p2 was set default")
	}
}
