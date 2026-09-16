package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func InitDB(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite works best with 1 writer or limited conns
	conn.SetConnMaxLifetime(time.Hour)

	database := &DB{conn}
	if err := database.migrate(); err != nil {
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	return database, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		display_name TEXT,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'operator',
		recovery_question TEXT DEFAULT '',
		recovery_answer_hash TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at DATETIME NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS models (
		id TEXT PRIMARY KEY,
		name TEXT,
		provider_id TEXT DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS traffic_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		api_key TEXT,
		api_key_name TEXT,
		model TEXT NOT NULL,
		prompt_tokens INTEGER DEFAULT 0,
		completion_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		status_code INTEGER NOT NULL,
		client_ip TEXT,
		stream INTEGER DEFAULT 0,
		error_message TEXT,
		level TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		key TEXT NOT NULL,
		prefix TEXT NOT NULL,
		name TEXT NOT NULL,
		is_active INTEGER DEFAULT 1,
		model_access_mode TEXT DEFAULT 'all',
		model_group_ids TEXT DEFAULT '[]',
		allowed_models TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS model_groups (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		models TEXT NOT NULL DEFAULT '[]',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS providers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		route TEXT NOT NULL,
		api_key TEXT,
		prefix TEXT NOT NULL DEFAULT '',
		is_default INTEGER DEFAULT 0,
		is_active INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS system_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		level TEXT NOT NULL,
		source TEXT NOT NULL,
		message TEXT NOT NULL,
		attrs TEXT DEFAULT '{}'
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_timestamp ON traffic_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_traffic_model ON traffic_logs(model);
	CREATE INDEX IF NOT EXISTS idx_traffic_api_key ON traffic_logs(api_key);
	CREATE INDEX IF NOT EXISTS idx_models_enabled ON models(enabled);
	CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key);
	CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
	CREATE INDEX IF NOT EXISTS idx_providers_prefix ON providers(prefix);
	CREATE INDEX IF NOT EXISTS idx_providers_active ON providers(is_active);
	CREATE INDEX IF NOT EXISTS idx_syslogs_timestamp ON system_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_syslogs_level ON system_logs(level);
	CREATE INDEX IF NOT EXISTS idx_syslogs_source ON system_logs(source);
	CREATE INDEX IF NOT EXISTS idx_model_groups_name ON model_groups(name);
	`
	if _, err := d.Exec(schema); err != nil {
		return err
	}

	// For existing databases, ensure columns exist
	_, _ = d.Exec("ALTER TABLE users ADD COLUMN recovery_question TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE users ADD COLUMN recovery_answer_hash TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN allowed_models TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN model_access_mode TEXT DEFAULT 'all'")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN model_group_ids TEXT DEFAULT '[]'")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN api_key_name TEXT")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN provider_id TEXT")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN level TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE models ADD COLUMN provider_id TEXT DEFAULT ''")
	_, _ = d.Exec("DELETE FROM providers WHERE id = 'dak' AND name = 'DAK Upstream'")
	_, _ = d.Exec("DELETE FROM models WHERE provider_id = 'dak' OR id LIKE 'dak/%'")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_name ON traffic_logs(api_key_name)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_level ON traffic_logs(level)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_status ON traffic_logs(status_code)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_provider ON traffic_logs(provider_id)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_id)")

	// Ensure at most one default provider exists across the database
	_, _ = d.Exec(`
		UPDATE providers SET is_default = 0
		WHERE id NOT IN (
			SELECT id FROM providers WHERE is_default = 1 ORDER BY updated_at DESC, created_at DESC LIMIT 1
		)
	`)
	// If providers exist but none is default, set the first active one as default
	_, _ = d.Exec(`
		UPDATE providers SET is_default = 1
		WHERE (SELECT COUNT(*) FROM providers WHERE is_default = 1) = 0
		AND id = (SELECT id FROM providers WHERE is_active = 1 ORDER BY created_at ASC LIMIT 1)
	`)

	return nil
}
