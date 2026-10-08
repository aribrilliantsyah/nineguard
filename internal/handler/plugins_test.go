package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/providers"
	"nineguard/internal/syslog"
	"nineguard/internal/traffic"
)

func setupTestHandler(t *testing.T) (*Handler, *db.DB) {
	tmpDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tmpDir, "handler_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	authMgr := auth.NewManager(database, true)
	modelsMgr := models.NewManager(database)
	trafficMgr := traffic.NewManager(database)
	syslogMgr := syslog.NewManager(database)
	keysMgr := keys.NewManager(database, "")
	providersMgr := providers.NewManager(database)
	pluginsMgr := plugins.NewManager(database, nil)

	h := New(authMgr, modelsMgr, trafficMgr, syslogMgr, keysMgr, providersMgr, nil, "")
	h.SetPlugins(pluginsMgr)
	return h, database
}

func contextWithUser(req *http.Request, user *auth.User) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), user)
	return req.WithContext(ctx)
}

func TestPluginHandlers_ListAndRBAC(t *testing.T) {
	h, db := setupTestHandler(t)
	defer db.Close()

	// 1. List plugins: public / any role
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	rec := httptest.NewRecorder()
	h.ListPlugins(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for list plugins, got %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Plugins []PluginWithMeta `json:"plugins"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&listResp)
	if len(listResp.Plugins) < 3 {
		t.Fatalf("expected at least 3 plugins, got %d", len(listResp.Plugins))
	}

	// 2. Create plugin as operator -> 403 Forbidden
	operator := &auth.User{ID: 2, Username: "op", Role: "operator"}
	admin := &auth.User{ID: 1, Username: "admin", Role: "admin"}

	createBody := []byte(`{"name":"PII Filter","url":"http://localhost:8080","category":"other"}`)
	reqOp := contextWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(createBody)), operator)
	recOp := httptest.NewRecorder()
	h.CreatePlugin(recOp, reqOp)

	if recOp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for operator, got %d", recOp.Code)
	}

	// 3. Create plugin as admin -> 201 Created with secret returned
	reqAdmin := contextWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/plugins", bytes.NewReader(createBody)), admin)
	recAdmin := httptest.NewRecorder()
	h.CreatePlugin(recAdmin, reqAdmin)

	if recAdmin.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for admin, got %d: %s", recAdmin.Code, recAdmin.Body.String())
	}
	var createResp struct {
		Plugin plugins.Plugin `json:"plugin"`
		Secret string         `json:"secret"`
	}
	_ = json.NewDecoder(recAdmin.Body).Decode(&createResp)
	if createResp.Secret == "" || createResp.Plugin.ID == "" {
		t.Fatalf("expected raw secret and ID returned, got %+v", createResp)
	}

	// 4. Delete built-in plugin -> 400 Bad Request
	reqDelBuiltin := contextWithUser(httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/caveman", nil), admin)
	reqDelBuiltin.SetPathValue("id", "caveman")
	recDelBuiltin := httptest.NewRecorder()
	h.DeletePlugin(recDelBuiltin, reqDelBuiltin)

	if recDelBuiltin.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request deleting built-in, got %d", recDelBuiltin.Code)
	}

	// 5. Delete HTTP plugin as admin -> 200 OK
	reqDel := contextWithUser(httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/"+createResp.Plugin.ID, nil), admin)
	reqDel.SetPathValue("id", createResp.Plugin.ID)
	recDel := httptest.NewRecorder()
	h.DeletePlugin(recDel, reqDel)

	if recDel.Code != http.StatusOK {
		t.Fatalf("expected 200 OK deleting http plugin, got %d: %s", recDel.Code, recDel.Body.String())
	}
}

func TestPluginHandlers_BindingsAndResolve(t *testing.T) {
	h, db := setupTestHandler(t)
	defer db.Close()

	// Upsert binding for Caveman
	bindBody := []byte(`{"scope_type":"global","scope_id":"","state":"on"}`)
	reqBind := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/caveman/bindings", bytes.NewReader(bindBody))
	reqBind.SetPathValue("id", "caveman")
	recBind := httptest.NewRecorder()
	h.UpsertPluginBinding(recBind, reqBind)

	if recBind.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on upsert binding, got %d: %s", recBind.Code, recBind.Body.String())
	}

	// Resolve preview for caveman
	reqRes := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/resolve?model=gpt-4o", nil)
	recRes := httptest.NewRecorder()
	h.ResolvePlugins(recRes, reqRes)

	if recRes.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on resolve, got %d: %s", recRes.Code, recRes.Body.String())
	}

	var resData struct {
		Plugins []plugins.ResolvedPlugin `json:"plugins"`
	}
	_ = json.NewDecoder(recRes.Body).Decode(&resData)

	foundCaveman := false
	for _, p := range resData.Plugins {
		if p.Plugin.ID == "caveman" {
			foundCaveman = true
			if p.EffectiveState != plugins.StateOn {
				t.Errorf("expected Caveman on after global binding upsert, got %s", p.EffectiveState)
			}
		}
	}
	if !foundCaveman {
		t.Errorf("expected Caveman in resolve results")
	}
}
