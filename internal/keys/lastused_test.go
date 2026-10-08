package keys_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
)

func TestLastUsedAtIsRFC3339UTC(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	km := keys.NewManager(database, "")
	k, err := km.CreateKey("pi-dev", "all", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO models (id, name, enabled) VALUES ('9r/claude', '9r/claude', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, status_code)
		VALUES ('2026-10-06 09:26:40', ?, 'pi-dev', ?, '9r/claude', 403)`, k.Key, k.ID); err != nil {
		t.Fatal(err)
	}

	list, err := km.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ki := range list {
		if ki.ID == k.ID {
			found = true
			if ki.LastUsedAt == nil || *ki.LastUsedAt != "2026-10-06T09:26:40Z" {
				t.Errorf("key last_used_at = %v, want 2026-10-06T09:26:40Z", ki.LastUsedAt)
			}
		}
	}
	if !found {
		t.Fatal("created key not listed")
	}

	ml, err := models.NewManager(database).ListModels("")
	if err != nil {
		t.Fatal(err)
	}
	for _, mi := range ml {
		if mi.ID == "9r/claude" {
			if mi.LastUsedAt == nil || *mi.LastUsedAt != "2026-10-06T09:26:40Z" {
				t.Errorf("model last_used_at = %v, want 2026-10-06T09:26:40Z", mi.LastUsedAt)
			}
			return
		}
	}
	t.Fatal("model 9r/claude not listed")
}
