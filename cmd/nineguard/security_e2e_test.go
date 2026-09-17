package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/syslog"
	"nineguard/internal/traffic"
	"nineguard/web"
)

func setupTestServer(t *testing.T) (http.Handler, *auth.Manager, *traffic.Manager, *keys.Manager, *providers.Manager, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "e2e_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	syslogMgr := syslog.NewManager(database)
	authMgr := auth.NewManager(database, true)
	keysMgr := keys.NewManager(database, "")
	providersMgr := providers.NewManager(database)
	modelsMgr := models.NewManager(database)
	trafficMgr := traffic.NewManager(database)

	revProxy, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	h := handler.New(authMgr, modelsMgr, trafficMgr, syslogMgr, keysMgr, providersMgr, revProxy, "")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /api/v1/auth/status", h.AuthStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", h.AuthSetup)
	mux.HandleFunc("POST /api/v1/auth/login", h.AuthLogin)
	mux.HandleFunc("GET /api/v1/config", h.Config)

	mux.HandleFunc("GET /api/v1/keys", h.ListKeys)
	mux.HandleFunc("POST /api/v1/keys", h.CreateKey)
	mux.HandleFunc("GET /api/v1/providers", h.ListProviders)
	mux.HandleFunc("GET /api/v1/traffic", h.GetTrafficLogs)
	mux.HandleFunc("GET /api/v1/models", h.ListModels)

	fileServer := web.StaticHandler()

	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/v1" || strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/") {
			revProxy.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "/api/v1/") || path == "/" || path == "/login" || path == "/setup" || path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	finalHandler := authMgr.RequireAuthMiddleware(rootHandler)
	cleanup := func() {
		syslogMgr.Close()
		database.Close()
	}

	return finalHandler, authMgr, trafficMgr, keysMgr, providersMgr, cleanup
}

func TestUnauthenticatedDashboardAPIAccessBlocked(t *testing.T) {
	finalHandler, authMgr, trafficMgr, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Initial setup
	_, _, err := authMgr.Setup("admin", "Admin123456")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 1. Unauthenticated attempt to generate key via POST /api/v1/keys -> MUST FAIL WITH 401
	keyPayload := map[string]interface{}{
		"name":              "Hacker Key",
		"model_access_mode": "all",
	}
	bodyBytes, _ := json.Marshal(keyPayload)
	reqCreateKey := httptest.NewRequest("POST", "/api/v1/keys", bytes.NewReader(bodyBytes))
	recCreateKey := httptest.NewRecorder()
	finalHandler.ServeHTTP(recCreateKey, reqCreateKey)

	if recCreateKey.Code != http.StatusUnauthorized {
		t.Fatalf("CRITICAL: Unauthenticated caller was able to reach /api/v1/keys! Got status %d, expected 401", recCreateKey.Code)
	}

	// 2. Unauthenticated attempt to read providers via GET /api/v1/providers -> MUST FAIL WITH 401
	reqProviders := httptest.NewRequest("GET", "/api/v1/providers", nil)
	recProviders := httptest.NewRecorder()
	finalHandler.ServeHTTP(recProviders, reqProviders)

	if recProviders.Code != http.StatusUnauthorized {
		t.Fatalf("CRITICAL: Unauthenticated caller was able to reach /api/v1/providers! Got status %d, expected 401", recProviders.Code)
	}

	// 3. Unauthenticated attempt to read traffic telemetry via GET /api/v1/traffic -> MUST FAIL WITH 401
	reqTraffic := httptest.NewRequest("GET", "/api/v1/traffic", nil)
	recTraffic := httptest.NewRecorder()
	finalHandler.ServeHTTP(recTraffic, reqTraffic)

	if recTraffic.Code != http.StatusUnauthorized {
		t.Fatalf("CRITICAL: Unauthenticated caller was able to reach /api/v1/traffic! Got status %d, expected 401", recTraffic.Code)
	}

	// 4. Proxy request without API key -> MUST FAIL WITH 401 and log to traffic
	reqProxy := httptest.NewRequest("GET", "/v1/models", nil)
	recProxy := httptest.NewRecorder()
	finalHandler.ServeHTTP(recProxy, reqProxy)

	if recProxy.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated proxy request, got %d", recProxy.Code)
	}

	// Verify traffic log entry recorded for the 401
	tLogs, total, err := trafficMgr.QueryLogs(traffic.FilterParams{})
	if err != nil || total < 1 || len(tLogs) < 1 {
		t.Fatalf("expected 401 proxy attempt to be recorded in traffic logs, got total %d", total)
	}
	if tLogs[0].StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 recorded in traffic logs, got %d", tLogs[0].StatusCode)
	}

	// 5. Legitimate Login: User logs in via POST /api/v1/auth/login
	loginPayload := map[string]string{"username": "admin", "password": "Admin123456"}
	loginBytes, _ := json.Marshal(loginPayload)
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBytes))
	recLogin := httptest.NewRecorder()
	finalHandler.ServeHTTP(recLogin, reqLogin)

	if recLogin.Code != http.StatusOK {
		t.Fatalf("login failed: %d: %s", recLogin.Code, recLogin.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, c := range recLogin.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("no session cookie received after login")
	}

	// 6. Authenticated user can now create key via POST /api/v1/keys
	reqCreateAuth := httptest.NewRequest("POST", "/api/v1/keys", bytes.NewReader(bodyBytes))
	reqCreateAuth.AddCookie(sessionCookie)
	recCreateAuth := httptest.NewRecorder()
	finalHandler.ServeHTTP(recCreateAuth, reqCreateAuth)

	if recCreateAuth.Code != http.StatusOK {
		t.Fatalf("authenticated key creation failed: %d: %s", recCreateAuth.Code, recCreateAuth.Body.String())
	}

	var createdKey keys.KeyInfo
	_ = json.NewDecoder(recCreateAuth.Body).Decode(&createdKey)
	if createdKey.RawKey == "" {
		t.Fatalf("expected created key to have raw key")
	}

	// 7. Proxy request WITH the newly generated client API key -> 200 OK
	reqProxyWithKey := httptest.NewRequest("GET", "/v1/models", nil)
	reqProxyWithKey.Header.Set("Authorization", "Bearer "+createdKey.RawKey)
	recProxyWithKey := httptest.NewRecorder()
	finalHandler.ServeHTTP(recProxyWithKey, reqProxyWithKey)

	if recProxyWithKey.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for proxy request with valid dashboard key, got %d: %s", recProxyWithKey.Code, recProxyWithKey.Body.String())
	}
}
