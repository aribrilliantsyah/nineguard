package syslog

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nineguard/internal/db"
)

func setupTestSyslogDB(t *testing.T) (*Manager, func()) {
	tmpDir, err := os.MkdirTemp("", "nineguard-syslog-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	mgr := NewManager(database)
	cleanup := func() {
		mgr.Close()
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return mgr, cleanup
}

func TestSyslogRecordAndQuery(t *testing.T) {
	mgr, cleanup := setupTestSyslogDB(t)
	defer cleanup()

	// Direct recording
	mgr.Record(SystemLogEntry{
		Level:   "INFO",
		Source:  "gateway",
		Message: "gateway started on :8080",
	})
	mgr.Record(SystemLogEntry{
		Level:   "WARN",
		Source:  "firewall",
		Message: "blocked request for model claude-3-opus",
	})
	mgr.Record(SystemLogEntry{
		Level:   "ERROR",
		Source:  "proxy",
		Message: "failed to connect upstream openai",
	})

	// Allow worker flush
	time.Sleep(300 * time.Millisecond)

	// Query all
	logs, total, err := mgr.QueryLogs(FilterParams{})
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}
	if total != 3 || len(logs) != 3 {
		t.Errorf("expected 3 logs, got total %d, len %d", total, len(logs))
	}

	// Filter by Source
	fwLogs, totalFw, err := mgr.QueryLogs(FilterParams{Source: "firewall"})
	if err != nil {
		t.Fatalf("QueryLogs source failed: %v", err)
	}
	if totalFw != 1 || len(fwLogs) != 1 || fwLogs[0].Level != "WARN" {
		t.Errorf("expected 1 firewall WARN log, got %d", totalFw)
	}

	// Filter by Level
	errLogs, totalErr, err := mgr.QueryLogs(FilterParams{Level: "ERROR"})
	if err != nil {
		t.Fatalf("QueryLogs level failed: %v", err)
	}
	if totalErr != 1 || len(errLogs) != 1 {
		t.Errorf("expected 1 ERROR log, got %d", totalErr)
	}

	// Volume test
	now := time.Now()
	vol, err := mgr.GetVolume(FilterParams{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	}, 10)
	if err != nil {
		t.Fatalf("GetVolume failed: %v", err)
	}
	if len(vol.Buckets) != 10 {
		t.Errorf("expected 10 buckets, got %d", len(vol.Buckets))
	}
	if vol.Totals["INFO"] != 1 || vol.Totals["WARN"] != 1 || vol.Totals["ERROR"] != 1 {
		t.Errorf("expected 1 INFO, 1 WARN, 1 ERROR, got totals: %+v", vol.Totals)
	}

	// Sources test
	sources, err := mgr.GetSources()
	if err != nil {
		t.Fatalf("GetSources failed: %v", err)
	}
	if len(sources) != 3 {
		t.Errorf("expected 3 sources, got %d: %+v", len(sources), sources)
	}
}

func TestSlogHandlerIntegration(t *testing.T) {
	mgr, cleanup := setupTestSyslogDB(t)
	defer cleanup()

	base := slog.NewTextHandler(os.Stdout, nil)
	logger := slog.New(NewSlogHandler(mgr, base))

	logger.Info("user authenticated", "source", "auth", "user", "admin")
	logger.Warn("high latency detected", "source", "proxy", "latency_ms", 1500)

	time.Sleep(300 * time.Millisecond)

	logs, total, err := mgr.QueryLogs(FilterParams{})
	if err != nil {
		t.Fatalf("QueryLogs failed: %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Errorf("expected 2 logs from slog, got %d", total)
	}
}
