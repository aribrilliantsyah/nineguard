package keys_test

import (
	"fmt"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

// A global schedule change pins every key that follows it in one transaction. This checks that
// it stays fast and correct with a large key table, and that it does not scan traffic_logs.
func TestSetGlobalScheduleScalesToManyKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	const n = 10000
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		custom := ""
		if i%10 == 0 {
			custom = `{"timezone":"Europe/Berlin","time":"09:00","weekday":2,"day":5}`
		}
		if _, err := tx.Exec(`INSERT INTO api_keys (id, key, prefix, name, is_active, quota_limit, quota_period, quota_reset_schedule)
			VALUES (?, ?, 'sk-ng-', ?, 1, 1000, 'monthly', NULLIF(?, ''))`,
			fmt.Sprintf("id%05d", i), fmt.Sprintf("sk-ng-scale%05d", i), fmt.Sprintf("k%d", i), custom); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// a big traffic table must not slow the write down: pins never read it
	tx, _ = d.Begin()
	for i := 0; i < 50000; i++ {
		if _, err := tx.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 10, CURRENT_TIMESTAMP)`,
			fmt.Sprintf("id%05d", i%n)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tx.Commit()

	mgr := keys.NewManager(d, "")
	start := time.Now()
	counts, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20))
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SetGlobalSchedule over %d keys (%d following): %v", n, counts.Following, took)
	if counts.Custom != n/10 || counts.Following != n-n/10 {
		t.Errorf("counts = %+v", counts)
	}

	var pinned, customPinned int
	_ = d.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE quota_window_pins IS NOT NULL AND quota_reset_schedule IS NULL`).Scan(&pinned)
	_ = d.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE quota_window_pins IS NOT NULL AND quota_reset_schedule IS NOT NULL`).Scan(&customPinned)
	if pinned != n-n/10 || customPinned != 0 {
		t.Errorf("pinned followers = %d (want %d), pinned custom keys = %d (want 0)", pinned, n-n/10, customPinned)
	}
	if took > 10*time.Second {
		t.Errorf("took %v for %d keys, too slow", took, n)
	}

	start = time.Now()
	if _, err := mgr.ClearScheduleOverrides(); err != nil {
		t.Fatal(err)
	}
	t.Logf("ClearScheduleOverrides over %d custom keys: %v", n/10, time.Since(start))

	// the proxy hot path is a cache hit; it must not pay for the schedule
	k, ok := mgr.ValidateClientKey("sk-ng-scale00007")
	if !ok || k.ResetRule.Schedule == nil {
		t.Fatalf("cache lookup after the write: %+v", k)
	}
}
