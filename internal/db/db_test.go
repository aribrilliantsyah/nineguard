package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPluginMigrationsAndSeeding(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_plugins.db")
	database, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Verify plugins table
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM plugins").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query plugins table: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected at least 3 seeded plugins, got %d", count)
	}

	// Verify global bindings
	err = database.QueryRow("SELECT COUNT(*) FROM plugin_bindings WHERE scope_type = 'global' AND state = 'off'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query plugin_bindings: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected 3 global off bindings, got %d", count)
	}

	// Verify model_groups priority column
	var priority int
	err = database.QueryRow("SELECT priority FROM model_groups LIMIT 1").Scan(&priority)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("column priority missing on model_groups: %v", err)
	}

	// Verify providers upstream_token_saving column
	var tokenSaving int
	err = database.QueryRow("SELECT upstream_token_saving FROM providers LIMIT 1").Scan(&tokenSaving)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("column upstream_token_saving missing on providers: %v", err)
	}

	// Verify traffic_logs columns
	var tokSaved int
	err = database.QueryRow("SELECT tokens_saved FROM traffic_logs LIMIT 1").Scan(&tokSaved)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("column tokens_saved missing on traffic_logs: %v", err)
	}
}
