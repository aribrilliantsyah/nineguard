package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/config"
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

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.LoadFromEnv()

	// 1. Initialize SQLite Database
	database, err := db.InitDB(cfg.DBFile)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}

	// 2. Initialize Managers
	syslogMgr := syslog.NewManager(database)
	defer syslogMgr.Close()

	// Capture all slog logs into syslog manager and stdout
	baseHandler := slog.NewTextHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(syslog.NewSlogHandler(syslogMgr, baseHandler)))

	authMgr := auth.NewManager(database, cfg.AuthEnabled)
	keysMgr := keys.NewManager(database, cfg.RouterAPIKey)
	routerTarget := keysMgr.GetUpstreamTarget(cfg.RouterTarget)
	providersMgr := providers.NewManager(database)
	modelsMgr := models.NewManager(database)
	trafficMgr := traffic.NewManager(database)

	// 3. Initialize Reverse Proxy
	revProxy, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr)
	if err != nil {
		slog.Error("failed to initialize reverse proxy", "error", err)
		os.Exit(1)
	}

	// 4. Handlers
	h := handler.New(authMgr, modelsMgr, trafficMgr, syslogMgr, keysMgr, providersMgr, revProxy, routerTarget)

	// Background Auto-Sync: automatically fetch models from active upstream providers
	go func() {
		time.Sleep(1200 * time.Millisecond)
		if added, removed, err := modelsMgr.SyncFromProviders(context.Background(), providersMgr); err == nil && (added > 0 || removed > 0) {
			slog.Info("auto-synced models from active upstream providers", "models_added", added, "models_cleaned", removed)
		}

		ticker := time.NewTicker(30 * time.Minute)
		for range ticker.C {
			_, _, _ = modelsMgr.SyncFromProviders(context.Background(), providersMgr)
		}
	}()

	mux := http.NewServeMux()

	// Public Health Probe
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Web UI Shell Pages with server-side auth guard
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if authMgr.IsAuthEnabled() {
			if needsSetup, _ := authMgr.NeedsSetup(); needsSetup {
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
			if user := auth.UserFromContext(r.Context()); user == nil {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
		}
		web.Page("index.html").ServeHTTP(w, r)
	})

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if !authMgr.IsAuthEnabled() {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if user := auth.UserFromContext(r.Context()); user != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if needsSetup, _ := authMgr.NeedsSetup(); needsSetup {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		web.Page("auth.html").ServeHTTP(w, r)
	})

	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) {
		if !authMgr.IsAuthEnabled() {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if user := auth.UserFromContext(r.Context()); user != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if needsSetup, _ := authMgr.NeedsSetup(); !needsSetup {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		web.Page("auth.html").ServeHTTP(w, r)
	})

	// REST API v1
	mux.HandleFunc("GET /api/v1/auth/status", h.AuthStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", h.AuthSetup)
	mux.HandleFunc("POST /api/v1/auth/login", h.AuthLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.AuthLogout)
	mux.HandleFunc("GET /api/v1/config", h.Config)

	// Protected endpoints (verified in handler / middleware)
	mux.HandleFunc("GET /api/v1/profile", h.GetProfile)
	mux.HandleFunc("POST /api/v1/profile/password", h.UpdatePassword)
	mux.HandleFunc("GET /api/v1/users", h.ListUsers)
	mux.HandleFunc("POST /api/v1/users", h.CreateUser)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.DeleteUser)

	mux.HandleFunc("GET /api/v1/models", h.ListModels)
	mux.HandleFunc("POST /api/v1/models/toggle", h.ToggleModel)
	mux.HandleFunc("POST /api/v1/models/sync", h.SyncModels)
	mux.HandleFunc("DELETE /api/v1/models/{id...}", h.DeleteModel)

	mux.HandleFunc("GET /api/v1/traffic", h.GetTrafficLogs)
	mux.HandleFunc("GET /api/v1/traffic/volume", h.GetTrafficVolume)
	mux.HandleFunc("GET /api/v1/traffic/export", h.ExportTrafficLogs)
	mux.HandleFunc("GET /api/v1/traffic/stats", h.GetTrafficStats)
	mux.HandleFunc("GET /api/v1/traffic/report", h.GetUsageReport)

	mux.HandleFunc("GET /api/v1/logs", h.GetSystemLogs)
	mux.HandleFunc("GET /api/v1/logs/volume", h.GetSystemLogVolume)
	mux.HandleFunc("GET /api/v1/logs/export", h.ExportSystemLogs)
	mux.HandleFunc("GET /api/v1/logs/sources", h.GetSystemLogSources)

	mux.HandleFunc("GET /api/v1/keys", h.ListKeys)
	mux.HandleFunc("POST /api/v1/keys", h.CreateKey)
	mux.HandleFunc("PUT /api/v1/keys/{id}", h.UpdateKey)
	mux.HandleFunc("POST /api/v1/keys/{id}", h.UpdateKey)
	mux.HandleFunc("POST /api/v1/keys/{id}/toggle", h.ToggleKey)
	mux.HandleFunc("DELETE /api/v1/keys/{id}", h.DeleteKey)

	mux.HandleFunc("GET /api/v1/providers", h.ListProviders)
	mux.HandleFunc("POST /api/v1/providers", h.CreateProvider)
	mux.HandleFunc("PUT /api/v1/providers/{id}", h.UpdateProvider)
	mux.HandleFunc("POST /api/v1/providers/{id}/toggle", h.ToggleProvider)
	mux.HandleFunc("DELETE /api/v1/providers/{id}", h.DeleteProvider)
	mux.HandleFunc("POST /api/v1/providers/test", h.TestProviderConnection)

	mux.HandleFunc("GET /api/v1/settings/upstream", h.GetUpstreamSettings)
	mux.HandleFunc("POST /api/v1/settings/upstream", h.SetUpstreamSettings)
	mux.HandleFunc("POST /api/v1/settings/upstream/test", h.TestUpstreamConnection)

	// Static Assets
	fileServer := web.StaticHandler()

	// Root Router: Dispatches between Proxy (/v1/*), API, Web UI, and Static assets
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Forward any /v1/ request (OpenAI standard) to NineGuard Proxy
		if path == "/v1" || strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/") {
			revProxy.ServeHTTP(w, r)
			return
		}

		// API, UI, and Static paths handled by mux
		if strings.HasPrefix(path, "/api/v1/") ||
			path == "/" || path == "/login" || path == "/setup" || path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}

		// Static assets
		fileServer.ServeHTTP(w, r)
	})

	// Wrap root with auth session middleware
	finalHandler := authMgr.Middleware(rootHandler)

	if cfg.RouterTarget != "" {
		slog.Info("NineGuard started", "port", cfg.Port, "target", cfg.RouterTarget, "auth", cfg.AuthEnabled)
	} else {
		slog.Info("NineGuard started", "port", cfg.Port, "auth", cfg.AuthEnabled)
	}
	if err := http.ListenAndServe(":"+cfg.Port, finalHandler); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
