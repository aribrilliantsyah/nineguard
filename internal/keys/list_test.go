package keys_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func newKeysDB(t *testing.T) (*db.DB, *keys.Manager) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	km := keys.NewManager(database, "")
	// Drop the auto-created default key so tests control the full set.
	if _, err := database.Exec("DELETE FROM api_keys"); err != nil {
		t.Fatal(err)
	}
	return database, km
}

// addKey inserts a key with a fixed created_at so ordering is deterministic.
func addKey(t *testing.T, database *db.DB, id, name, mode string, active bool, created string) {
	t.Helper()
	act := 0
	if active {
		act = 1
	}
	_, err := database.Exec(`INSERT INTO api_keys (id, key, prefix, name, is_active, model_access_mode, created_at, updated_at)
		VALUES (?, ?, 'sk-ng-', ?, ?, ?, ?, ?)`, id, "sk-ng-"+id+"-raw-key-0000", name, act, mode, created, created)
	if err != nil {
		t.Fatal(err)
	}
}

func addTraffic(t *testing.T, database *db.DB, keyID, ts string, status, tokens int) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, status_code, total_tokens)
		VALUES (?, 'masked', 'whatever', ?, 'm', ?, ?)`, ts, keyID, status, tokens)
	if err != nil {
		t.Fatal(err)
	}
}

func TestListKeysStatsByKeyID(t *testing.T) {
	database, km := newKeysDB(t)
	addKey(t, database, "a", "pi-dev", "all", true, "2026-10-01 00:00:00")
	addKey(t, database, "b", "pi-dev", "all", true, "2026-10-02 00:00:00") // same name, separate stats
	addTraffic(t, database, "a", "2026-10-06 09:26:40", 403, 0)
	addTraffic(t, database, "a", "2026-10-05 09:00:00", 200, 50)

	list, err := km.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" {
		t.Fatalf("order = %v", ids(list))
	}
	a := list[1]
	if a.TotalRequests != 2 || a.TotalTokens != 50 {
		t.Errorf("a stats = %d req %d tok", a.TotalRequests, a.TotalTokens)
	}
	if a.LastUsedAt == nil || *a.LastUsedAt != "2026-10-06T09:26:40Z" {
		t.Errorf("a last_used_at = %v", a.LastUsedAt)
	}
	b := list[0]
	if b.TotalRequests != 0 || b.LastUsedAt != nil {
		t.Errorf("b should have no stats, got %d req, last %v", b.TotalRequests, b.LastUsedAt)
	}

	// Renaming keeps stats.
	if _, err := km.UpdateKey("a", "pi-dev-renamed", "all", nil, nil); err != nil {
		t.Fatal(err)
	}
	list, _ = km.ListKeys()
	for _, k := range list {
		if k.ID == "a" && k.TotalRequests != 2 {
			t.Errorf("renamed key lost stats: %d", k.TotalRequests)
		}
	}
}

func ids(list []keys.KeyInfo) []string {
	out := make([]string, len(list))
	for i, k := range list {
		out[i] = k.ID
	}
	return out
}

func idsStr(list []keys.KeyInfo) string { return fmt.Sprint(ids(list)) }
