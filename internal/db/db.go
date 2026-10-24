package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nineguard/internal/vault"
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
	if err := vault.Init(filepath.Dir(dbPath)); err != nil {
		return nil, fmt.Errorf("vault init failed: %w", err)
	}
	if err := database.migrate(); err != nil {
		return nil, fmt.Errorf("database migration failed: %w", err)
	}
	if err := database.migrateSecretsToVault(); err != nil {
		return nil, fmt.Errorf("secret vault migration failed: %w", err)
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
		quota_type TEXT DEFAULT 'disabled',
		quota_auth_secret TEXT DEFAULT '',
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

	CREATE TABLE IF NOT EXISTS model_prices (
		family TEXT PRIMARY KEY,
		input_per_mtok REAL NOT NULL,
		output_per_mtok REAL NOT NULL,
		cache_read_per_mtok REAL,
		cache_write_per_mtok REAL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS model_price_overrides (
		model_id TEXT PRIMARY KEY,
		input_per_mtok REAL NOT NULL,
		output_per_mtok REAL NOT NULL,
		cache_read_per_mtok REAL,
		cache_write_per_mtok REAL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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

	// Model quotas (ADR 0007/0008)
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_mode TEXT DEFAULT 'key'")
	if _, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS api_key_model_quotas (
			api_key_id   TEXT NOT NULL,
			model_id     TEXT NOT NULL COLLATE NOCASE,
			quota_limit  INTEGER NOT NULL CHECK (quota_limit > 0),
			quota_period TEXT NOT NULL CHECK (quota_period IN ('daily','weekly','monthly','total')),
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (api_key_id, model_id)
		)`); err != nil {
		return fmt.Errorf("create api_key_model_quotas: %w", err)
	}
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN model_id TEXT COLLATE NOCASE")
	_, _ = d.Exec("CREATE INDEX IF NOT EXISTS idx_traffic_key_model_ts ON traffic_logs(api_key_id, model_id, timestamp)")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('quota_warn_percent', '80')")

	// Quota reset schedule (ADR 0010): the key's own schedule (JSON, NULL = follow the global
	// schedule) and its Transitional Windows (JSON by Quota Period, NULL = none).
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_reset_schedule TEXT")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN quota_window_pins TEXT")

	// Upstream quotas (ADR 0009)
	_, _ = d.Exec("ALTER TABLE providers ADD COLUMN quota_type TEXT DEFAULT 'disabled'")
	_, _ = d.Exec("ALTER TABLE providers ADD COLUMN quota_auth_secret TEXT DEFAULT ''")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('chat_commands_enabled', 'true')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('upstream_quota_sync_interval', '10m')")

	// Usage Diagnostics (spec 2026-10-19)
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN api_format TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN cache_read_tokens INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN cache_write_tokens INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN reasoning_tokens INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN thinking_requested INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN thinking_delivered INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN served_model TEXT DEFAULT ''")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN model_fallback INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN usage_estimated INTEGER DEFAULT 0")
	_, _ = d.Exec("ALTER TABLE traffic_logs ADD COLUMN cost_usd REAL")
	_, _ = d.Exec("ALTER TABLE api_keys ADD COLUMN cost_visibility TEXT")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('stream_usage_injection', 'true')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('price_list_url', 'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('price_list_synced_at', '')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('display_currency', 'USD')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('fx_rate_manual', '')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('fx_rate_auto', '')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('fx_rate_synced_at', '')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('cost_visible_to_keys', 'true')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('session_gap_minutes', '30')")
	_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('usage_comparison_enabled', 'true')")

	// Instance ID for loop detection
	var instID string
	_ = d.QueryRow("SELECT value FROM settings WHERE key = 'instance_id'").Scan(&instID)
	if instID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		newInstID := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		_, _ = d.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('instance_id', ?)", newInstID)
	}

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
				default_settings = COALESCE(NULLIF(plugins.default_settings, ''), excluded.default_settings)
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

// GetInstanceID returns the unique instance UUID for loop detection.
func (d *DB) GetInstanceID() string {
	var id string
	_ = d.QueryRow("SELECT value FROM settings WHERE key = 'instance_id'").Scan(&id)
	return id
}

// migrateSecretsToVault migrates unencrypted credentials in SQLite to AES-256-GCM.
func (d *DB) migrateSecretsToVault() error {
	// 1. Providers: api_key and quota_auth_secret
	rows, err := d.Query("SELECT id, COALESCE(api_key, ''), COALESCE(quota_auth_secret, '') FROM providers")
	if err == nil {
		type provSecret struct {
			id, key, secret string
		}
		var list []provSecret
		for rows.Next() {
			var ps provSecret
			if err := rows.Scan(&ps.id, &ps.key, &ps.secret); err == nil {
				list = append(list, ps)
			}
		}
		rows.Close()

		for _, ps := range list {
			needsUpdate := false
			encKey := ps.key
			if ps.key != "" && !strings.HasPrefix(ps.key, vault.Prefix) {
				if enc, err := vault.Encrypt(ps.key); err == nil {
					encKey = enc
					needsUpdate = true
				}
			}
			encSecret := ps.secret
			if ps.secret != "" && !strings.HasPrefix(ps.secret, vault.Prefix) {
				if enc, err := vault.Encrypt(ps.secret); err == nil {
					encSecret = enc
					needsUpdate = true
				}
			}
			if needsUpdate {
				_, _ = d.Exec("UPDATE providers SET api_key = ?, quota_auth_secret = ? WHERE id = ?", encKey, encSecret, ps.id)
			}
		}
	}

	// 2. Settings: router_api_key
	var routerKey string
	if err := d.QueryRow("SELECT value FROM settings WHERE key = 'router_api_key'").Scan(&routerKey); err == nil {
		if routerKey != "" && !strings.HasPrefix(routerKey, vault.Prefix) {
			if enc, err := vault.Encrypt(routerKey); err == nil {
				_, _ = d.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'router_api_key'", enc)
			}
		}
	}

	// 3. Plugins: secret
	pRows, err := d.Query("SELECT id, COALESCE(secret, '') FROM plugins WHERE secret != ''")
	if err == nil {
		type plugSecret struct {
			id, secret string
		}
		var pList []plugSecret
		for pRows.Next() {
			var pl plugSecret
			if err := pRows.Scan(&pl.id, &pl.secret); err == nil {
				pList = append(pList, pl)
			}
		}
		pRows.Close()

		for _, pl := range pList {
			if pl.secret != "" && !strings.HasPrefix(pl.secret, vault.Prefix) {
				if enc, err := vault.Encrypt(pl.secret); err == nil {
					_, _ = d.Exec("UPDATE plugins SET secret = ? WHERE id = ?", enc, pl.id)
				}
			}
		}
	}

	return nil
}


