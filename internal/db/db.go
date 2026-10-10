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
		level TEXT DEFAULT '',
		has_images INTEGER DEFAULT 0,
		image_count INTEGER DEFAULT 0
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
		quota_limit INTEGER DEFAULT 0,
		quota_period TEXT DEFAULT 'none',
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

	CREATE TABLE IF NOT EXISTS plugins (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		url TEXT DEFAULT '',
		secret TEXT DEFAULT '',
		timeout_ms INTEGER DEFAULT 3000,
		failure_policy TEXT DEFAULT 'open',
		bypassable INTEGER DEFAULT 1,
		pipeline_order INTEGER DEFAULT 100,
		category TEXT NOT NULL DEFAULT 'other',
		summary TEXT DEFAULT '',
		default_settings TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS plugin_bindings (
		plugin_id TEXT NOT NULL,
		scope_type TEXT NOT NULL,
		scope_id TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'inherit',
		settings TEXT DEFAULT '{}',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (plugin_id, scope_type, scope_id)
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
	CREATE INDEX IF NOT EXISTS idx_plugins_order ON plugins(pipeline_order);
	CREATE INDEX IF NOT EXISTS idx_plugin_bindings_plugin ON plugin_bindings(plugin_id);
	CREATE INDEX IF NOT EXISTS idx_plugin_bindings_scope ON plugin_bindings(scope_type, scope_id);

	CREATE TABLE IF NOT EXISTS traffic_payloads (
		traffic_id INTEGER PRIMARY KEY,
		request_body BLOB,
		response_body BLOB,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(traffic_id) REFERENCES traffic_logs(id) ON DELETE CASCADE
	);
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
	_, _ = d.Exec("ALTER TABLE models ADD COLUMN removed_at DATETIME")
	_, _ = d.Exec("DELETE FROM providers WHERE id = 'dak' AND name = 'DAK Upstream'")
	_, _ = d.Exec("DELETE FROM models WHERE provider_id = 'dak' OR id LIKE 'dak/%'")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_name ON traffic_logs(api_key_name)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_level ON traffic_logs(level)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_status ON traffic_logs(status_code)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_provider ON traffic_logs(provider_id)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_id)")

	// Traffic linked to API keys by ID (ADR 0003).
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN api_key_id TEXT")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_id ON traffic_logs(api_key_id)")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_id_ts ON traffic_logs(api_key_id, timestamp)")
	if err := d.backfillTrafficKeyID(); err != nil {
		return fmt.Errorf("backfill traffic_logs.api_key_id: %w", err)
	}

	// Plugin system schema extensions
	_, _ = d.Exec("ALTER TABLE model_groups ADD COLUMN priority INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE providers ADD COLUMN upstream_token_saving INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE providers ADD COLUMN upstream_token_saving_note TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN plugins_skipped TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN plugins_applied TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN tokens_saved INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN tokens_overhead INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN plugin_errors TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN plugin_ms INTEGER DEFAULT 0")

	// Multimodal image logging (Feature 1)
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN has_images INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN image_count INTEGER DEFAULT 0")

	// Payload Inspector & Replay (Feature 6)
	_, _ = d.Exec(`
		CREATE TABLE IF NOT EXISTS traffic_payloads (
			traffic_id INTEGER PRIMARY KEY,
			request_body BLOB,
			response_body BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(traffic_id) REFERENCES traffic_logs(id) ON DELETE CASCADE
		)
	`)
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('record_payloads', 'disabled')")

	// Token quota & spike alerts (ADR 0005)
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_limit INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_period TEXT DEFAULT 'none'")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('heavy_token_threshold', '8000')")

	if err := d.seedPlugins(); err != nil {
		return fmt.Errorf("seed plugins: %w", err)
	}

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

// BackfilledTrafficKeyIDs is the number of traffic rows linked to a key by the
// one-time backfill during this process's InitDB (0 when the backfill already
// ran earlier). main logs it after the system log is ready.
var BackfilledTrafficKeyIDs int64

// backfillTrafficKeyID links historical traffic rows to API keys by matching
// key name plus the last 4 characters of the key. Rows with zero or several
// matching keys stay NULL. Runs once, guarded by a settings row.
func (d *DB) backfillTrafficKeyID() error {
	BackfilledTrafficKeyIDs = 0
	var done string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'migration.traffic_key_id'").Scan(&done)
	if err == nil && done == "done" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	res, err := d.Exec(`
		UPDATE traffic_logs
		SET api_key_id = (
			SELECT k.id FROM api_keys k
			WHERE k.name = traffic_logs.api_key_name
			  AND substr(k.key, -4) = substr(traffic_logs.api_key, -4)
		)
		WHERE api_key_id IS NULL
		  AND (
			SELECT COUNT(*) FROM api_keys k
			WHERE k.name = traffic_logs.api_key_name
			  AND substr(k.key, -4) = substr(traffic_logs.api_key, -4)
		  ) = 1
	`)
	if err != nil {
		return err
	}
	BackfilledTrafficKeyIDs, _ = res.RowsAffected()

	_, err = d.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES ('migration.traffic_key_id', 'done', CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = 'done', updated_at = CURRENT_TIMESTAMP
	`)
	return err
}

func (d *DB) seedPlugins() error {
	builtins := []struct {
		id, name, desc, category string
		order                    int
		settings                 string
		bypassable               int
		failurePolicy            string
	}{
		{"secretguard", "SecretGuard", "Scans prompts for API keys, private keys, and environment secrets", "other", 5, `{"action":"block"}`, 0, "closed"},
		{"headroom", "Headroom", "Compresses message history before forwarding to model", "input_compression", 10, `{"url":"http://127.0.0.1:8787","mode":"incremental","compress_user_messages":false}`, 1, "open"},
		{"ponytail", "Ponytail", "Instructs model to respond with compact formatting", "output_style", 20, `{"level":"full"}`, 1, "open"},
		{"caveman", "Caveman", "Instructs model to respond in terse caveman style", "output_style", 30, `{"variant":"caveman"}`, 1, "open"},
	}
	for _, b := range builtins {
		_, err := d.Exec(`
			INSERT INTO plugins (id, kind, name, description, category, bypassable, pipeline_order, failure_policy, default_settings, created_at, updated_at)
			VALUES (?, 'builtin', ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				category = excluded.category,
				bypassable = excluded.bypassable,
				pipeline_order = excluded.pipeline_order,
				failure_policy = excluded.failure_policy,
				default_settings = excluded.default_settings
		`, b.id, b.name, b.desc, b.category, b.bypassable, b.order, b.failurePolicy, b.settings)
		if err != nil {
			return err
		}

		_, err = d.Exec(`
			INSERT INTO plugin_bindings (plugin_id, scope_type, scope_id, state, settings, updated_at)
			VALUES (?, 'global', '', 'off', '{}', CURRENT_TIMESTAMP)
			ON CONFLICT(plugin_id, scope_type, scope_id) DO NOTHING
		`, b.id)
		if err != nil {
			return err
		}
	}
	return nil
}

