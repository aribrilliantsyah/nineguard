package db_test

import (
	"testing"

	"nineguard/internal/db"
)

func TestResetScheduleColumnsAreNullByDefault(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	if _, err := d.Exec(`INSERT INTO api_keys (id, key, prefix, name) VALUES ('k1', 'sk-ng-test1', 'sk-ng-', 'old key')`); err != nil {
		t.Fatalf("insert key: %v", err)
	}
	var sched, pins *string
	if err := d.QueryRow(`SELECT quota_reset_schedule, quota_window_pins FROM api_keys WHERE id = 'k1'`).Scan(&sched, &pins); err != nil {
		t.Fatalf("reset schedule columns missing: %v", err)
	}
	if sched != nil || pins != nil {
		t.Errorf("existing/new keys must have NULL schedule and pins, got %v %v", sched, pins)
	}
}

// migrate() runs on every start; a second run on the same file must not fail or lose data.
func TestResetScheduleMigrationIsIdempotent(t *testing.T) {
	path := t.TempDir() + "/ng.db"
	d, err := db.InitDB(path)
	if err != nil {
		t.Fatalf("first InitDB: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO api_keys (id, key, prefix, name, quota_reset_schedule) VALUES ('k1', 'sk-ng-a', 'sk-ng-', 'a', '{"timezone":"UTC"}')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	d.Close()

	d2, err := db.InitDB(path)
	if err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
	defer d2.Close()
	var sched string
	if err := d2.QueryRow(`SELECT quota_reset_schedule FROM api_keys WHERE id = 'k1'`).Scan(&sched); err != nil || sched != `{"timezone":"UTC"}` {
		t.Errorf("schedule lost across migrations: %q %v", sched, err)
	}
}
