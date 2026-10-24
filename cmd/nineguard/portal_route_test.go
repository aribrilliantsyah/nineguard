package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/syslog"
	"nineguard/internal/traffic"
	"nineguard/web"
)

func TestPortalRoute(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "portal_test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	syslogMgr := syslog.NewManager(database)
	defer syslogMgr.Close()

	authMgr := auth.NewManager(database, true)
	keysMgr := keys.NewManager(database, "")
	providersMgr := providers.NewManager(database)
	modelsMgr := models.NewManager(database)
	trafficMgr := traffic.NewManager(database)
	pluginsMgr := plugins.NewManager(database, nil)

	revProxy, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, pluginsMgr)
	if err != nil {
		t.Fatalf("failed to init proxy: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /portal", func(w http.ResponseWriter, r *http.Request) {
		web.Page("portal.html").ServeHTTP(w, r)
	})

	fileServer := web.StaticHandler()

	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/v1" || strings.HasPrefix(path, "/v1/") {
			revProxy.ServeHTTP(w, r)
			return
		}
		if path == "/portal" {
			mux.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	finalHandler := authMgr.RequireAuthMiddleware(rootHandler)

	// Make request without any admin cookie
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rec := httptest.NewRecorder()

	finalHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for public /portal route, got %d", rec.Code)
	}
}
