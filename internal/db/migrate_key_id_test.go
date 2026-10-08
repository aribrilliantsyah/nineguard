package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func columnExists(t *testing.T, d *DB, table, col string) bool {
	t.Helper()
	rows, err := d.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		if name == col {
			return true
		}
	}
	return false
}

func indexExists(t *testing.T, d *DB, name string) bool {
	t.Helper()
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&n)
	return n == 1
}

func TestTrafficKeyIDColumnAndIndexes(t *testing.T) {
	d, err := InitDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !columnExists(t, d, "traffic_logs", "api_key_id") {
		t.Error("traffic_logs.api_key_id missing")
	}
	for _, idx := range []string{"idx_traffic_key_id", "idx_traffic_key_id_ts"} {
		if !indexExists(t, d, idx) {
			t.Errorf("index %s missing", idx)
		}
	}
	var v string
	if err := d.QueryRow("SELECT value FROM settings WHERE key = 'migration.traffic_key_id'").Scan(&v); err != nil || v != "done" {
		t.Errorf("guard row = %q, %v", v, err)
	}
}

func TestTrafficKeyIDBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	d, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-migration database: clear guard and links.
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec("DELETE FROM settings WHERE key = 'migration.traffic_key_id'")
	mustExec(`INSERT INTO api_keys (id, key, prefix, name) VALUES
		('k-pi',  'sk-ng-aaaaaaaaaaaa1111', 'sk-ng-aaaaaa', 'pi-dev'),
		('k-mar', 'sk-ng-bbbbbbbbbbbb2222', 'sk-ng-bbbbbb', 'marinara'),
		('k-d1',  'sk-ng-cccccccccccc3333', 'sk-ng-cccccc', 'dup'),
		('k-d2',  'sk-ng-dddddddddddd3333', 'sk-ng-dddddd', 'dup')`)
	ins := func(masked, name string) {
		mustExec(`INSERT INTO traffic_logs (api_key, api_key_name, model, status_code) VALUES (?, ?, 'm', 200)`, masked, name)
	}
	ins("sk-ng-...1111", "pi-dev")   // unique match -> k-pi
	ins("sk-ng-...1111", "pi-dev")   // unique match -> k-pi
	ins("sk-ng-...2222", "marinara") // unique match -> k-mar
	ins("sk-ng-...9999", "pi-dev")   // suffix mismatch -> NULL
	ins("sk-ng-...3333", "dup")      // two keys match -> NULL (ambiguous)
	ins("sk-ng-...1111", "old-name") // name mismatch -> NULL
	ins("unauthorized", "Unknown")   // 401 row -> NULL
	d.Close()

	d, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if BackfilledTrafficKeyIDs != 3 {
		t.Errorf("BackfilledTrafficKeyIDs = %d, want 3", BackfilledTrafficKeyIDs)
	}
	count := func(where string) int {
		var n int
		_ = d.QueryRow("SELECT COUNT(*) FROM traffic_logs WHERE " + where).Scan(&n)
		return n
	}
	if n := count("api_key_id = 'k-pi'"); n != 2 {
		t.Errorf("k-pi rows = %d, want 2", n)
	}
	if n := count("api_key_id = 'k-mar'"); n != 1 {
		t.Errorf("k-mar rows = %d, want 1", n)
	}
	if n := count("api_key_id IS NULL"); n != 4 {
		t.Errorf("unlinked rows = %d, want 4", n)
	}

	// Guard: a later run must not touch rows even if they would now match.
	mustExec2 := func(q string) {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mustExec2(`INSERT INTO traffic_logs (api_key, api_key_name, model, status_code) VALUES ('sk-ng-...2222', 'marinara', 'm', 200)`)
	d.Close()
	d2, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if BackfilledTrafficKeyIDs != 0 {
		t.Errorf("second run backfilled %d rows, want 0", BackfilledTrafficKeyIDs)
	}
	var id sql.NullString
	_ = d2.QueryRow("SELECT api_key_id FROM traffic_logs ORDER BY id DESC LIMIT 1").Scan(&id)
	if id.Valid {
		t.Errorf("guarded run linked a new row: %v", id.String)
	}
}
