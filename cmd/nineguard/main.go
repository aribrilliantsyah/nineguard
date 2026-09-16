package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

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
	"nineguard/internal/tray"
	"nineguard/internal/ui"
	"nineguard/internal/version"
	"nineguard/web"
)

func printHelp() {
	fmt.Printf(`NineGuard - AI Gateway, Proxy & Firewall (%s)

Usage:
  nineguard [options]

Options:
  -p, --port <port>       Set HTTP server listening port (default: 8080)
  -t, --tray              Run directly in system tray mode (background)
  -l, --logs              Start server and stream live running logs
  -d, --daemon            Run in headless/daemon mode (no interactive TUI)
  -h, --help              Show this help message

Environment Variables:
  NINEGUARD_PORT          Server port (default: 8080)
  NINEGUARD_AUTH_ENABLED  Enable dashboard authentication (default: true)
  NINEGUARD_DB_FILE       SQLite database path (default: ./data/nineguard.db)
  NINEGUARD_ROUTER_TARGET Target 9Router or OpenAI-compatible upstream endpoint
`, version.Version)
}

func main() {
	var (
		flagPort   string
		flagTray   bool
		flagLogs   bool
		flagDaemon bool
	)

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch arg {
		case "-p", "--port":
			if i+1 < len(os.Args) {
				flagPort = os.Args[i+1]
				i++
			}
		case "-t", "--tray":
			flagTray = true
		case "-l", "--logs":
			flagLogs = true
		case "-d", "--daemon", "--headless":
			flagDaemon = true
		case "-h", "--help":
			printHelp()
			return
		}
	}

	cfg := config.LoadFromEnv()
	if flagPort != "" {
		cfg.Port = flagPort
	}

	// 1. Initialize SQLite Database
	database, err := db.InitDB(cfg.DBFile)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}

	// 2. Initialize Syslog and LogHub
	syslogMgr := syslog.NewManager(database)
	defer syslogMgr.Close()

	logHub := ui.NewLogHub(250)

	isTTY := isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
	isInteractive := isTTY && !flagTray && !flagLogs && !flagDaemon

	if isInteractive {
		logHub.SetEchoStdout(false)
	} else {
		logHub.SetEchoStdout(true)
	}

	// Wrap slog: records to syslogMgr (SQLite) and LogHub (live UI / stream)
	baseHandler := ui.NewLogHubHandler(logHub, nil)
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
	mux.HandleFunc("GET /api/v1/auth/recovery", h.GetRecoveryQuestion)
	mux.HandleFunc("POST /api/v1/auth/recovery", h.RecoverPassword)
	mux.HandleFunc("GET /api/v1/config", h.Config)

	// Protected endpoints (verified in handler / middleware)
	mux.HandleFunc("GET /api/v1/profile", h.GetProfile)
	mux.HandleFunc("PATCH /api/v1/profile", h.UpdateProfile)
	mux.HandleFunc("POST /api/v1/profile", h.UpdateProfile)
	mux.HandleFunc("POST /api/v1/profile/password", h.UpdatePassword)
	mux.HandleFunc("POST /api/v1/profile/recovery", h.SetRecoveryQuestion)
	mux.HandleFunc("GET /api/v1/users", h.ListUsers)
	mux.HandleFunc("POST /api/v1/users", h.CreateUser)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.DeleteUser)

	mux.HandleFunc("GET /api/v1/models", h.ListModels)
	mux.HandleFunc("POST /api/v1/models/toggle", h.ToggleModel)
	mux.HandleFunc("POST /api/v1/models/sync", h.SyncModels)
	mux.HandleFunc("DELETE /api/v1/models/{id...}", h.DeleteModel)

	mux.HandleFunc("GET /api/v1/model-groups", h.ListModelGroups)
	mux.HandleFunc("POST /api/v1/model-groups", h.CreateModelGroup)
	mux.HandleFunc("GET /api/v1/model-groups/{id}", h.GetModelGroup)
	mux.HandleFunc("PUT /api/v1/model-groups/{id}", h.UpdateModelGroup)
	mux.HandleFunc("POST /api/v1/model-groups/{id}", h.UpdateModelGroup)
	mux.HandleFunc("DELETE /api/v1/model-groups/{id}", h.DeleteModelGroup)

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
	mux.HandleFunc("POST /api/v1/providers/{id}", h.UpdateProvider)
	mux.HandleFunc("POST /api/v1/providers/{id}/toggle", h.ToggleProvider)
	mux.HandleFunc("POST /api/v1/providers/{id}/default", h.SetDefaultProvider)
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

	finalHandler := authMgr.Middleware(rootHandler)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: finalHandler,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	// Wait up to 2 seconds for server port to be active
	ready := false
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+cfg.Port, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
	}
	if !ready {
		select {
		case err := <-serverErrCh:
			slog.Error("failed to start HTTP server", "port", cfg.Port, "error", err)
			os.Exit(1)
		default:
		}
	}

	if cfg.RouterTarget != "" {
		slog.Info("NineGuard started", "port", cfg.Port, "target", cfg.RouterTarget, "auth", cfg.AuthEnabled)
	} else {
		slog.Info("NineGuard started", "port", cfg.Port, "auth", cfg.AuthEnabled)
	}

	// 5. Handle Execution Modes

	// Mode A: Direct Tray
	if flagTray {
		slog.Info("NineGuard running in system tray mode", "port", cfg.Port)
		tray.Run(tray.Options{
			Port:      cfg.Port,
			ServerURL: "http://localhost:" + cfg.Port,
			OnOpenDashboard: func() {
				_ = ui.OpenBrowser("http://localhost:" + cfg.Port)
			},
			OnQuit: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
				os.Exit(0)
			},
		})
		return
	}

	// Mode B: Direct Logs
	if flagLogs {
		kr := ui.GetKeyReader()
		exit, _ := ui.ViewLogs(kr, logHub)
		if exit {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}
		return
	}

	// Mode C: Headless / Daemon (Docker, systemd, or --daemon)
	if !isInteractive {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down NineGuard server...")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return
	}

	// Mode D: Interactive Selection Menu
	termSession, _ := ui.EnterRawMode()
	defer func() {
		fmt.Print("\033[?25h")
		if termSession != nil {
			termSession.Restore()
		}
	}()

	kr := ui.GetKeyReader()

menuLoop:
	for {
		action, err := ui.ShowMenu(kr, ui.MenuConfig{
			Version:      version.Version,
			Port:         cfg.Port,
			RouterTarget: cfg.RouterTarget,
		})
		if err != nil || action == ui.ActionExit {
			break menuLoop
		}

		switch action {
		case ui.ActionWeb:
			nextAction, _ := ui.ShowWebPrompt(kr, cfg.Port)
			if nextAction == ui.ActionLogs {
				exit, _ := ui.ViewLogs(kr, logHub)
				if exit {
					break menuLoop
				}
			} else if nextAction == ui.ActionExit {
				break menuLoop
			}
		case ui.ActionLogs:
			exit, _ := ui.ViewLogs(kr, logHub)
			if exit {
				break menuLoop
			}
		case ui.ActionTray:
			shouldExit, _ := ui.HandleTraySelection(kr, cfg.Port, server, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
				os.Exit(0)
			})
			if shouldExit {
				return
			}
		}
	}

	fmt.Print("\r\n\033[2mShutting down NineGuard...\033[0m\r\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
