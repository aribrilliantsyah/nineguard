package keys_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
)

func TestKeyAllowListSkipsRemovedModels(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "rm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, q := range []string{
		`INSERT INTO models (id, name, provider_id, enabled) VALUES ('p1/live', 'p1/live', 'p1', 1)`,
		`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p1/gone', 'p1/gone', 'p1', 1, CURRENT_TIMESTAMP)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	modelsMgr := models.NewManager(database)
	keysMgr := keys.NewManager(database, "")

	grp, err := modelsMgr.CreateGroup("G", "", []string{"p1/live", "p1/gone", "p2/*"})
	if err != nil {
		t.Fatal(err)
	}
	keysMgr.ReloadGroupCache()

	groupKey, err := keysMgr.CreateKey("grp", "group", []string{grp.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	customKey, err := keysMgr.CreateKey("cus", "custom", nil, []string{"p1/live", "p1/gone"})
	if err != nil {
		t.Fatal(err)
	}

	for name, raw := range map[string]string{"group": groupKey.RawKey, "custom": customKey.RawKey} {
		ki, ok := keysMgr.ValidateClientKey(raw)
		if !ok {
			t.Fatalf("%s key invalid", name)
		}
		if !ki.IsModelAllowed("p1/live") {
			t.Errorf("%s: live model must stay allowed", name)
		}
		if ki.IsModelAllowed("p1/gone") {
			t.Errorf("%s: removed model must not be allowed", name)
		}
		for _, e := range ki.GetEffectiveAllowedModels() {
			if e == "p1/gone" {
				t.Errorf("%s: effective list still has the removed model", name)
			}
		}
	}

	// Wildcard entry is never flagged: another provider's model still passes.
	gk, _ := keysMgr.ValidateClientKey(groupKey.RawKey)
	if !gk.IsModelAllowed("p2/anything") {
		t.Error("wildcard entry must keep matching")
	}

	// Model comes back: the allow list recovers after the cache refresh a sync triggers.
	if _, err := database.Exec(`UPDATE models SET removed_at = NULL WHERE id = 'p1/gone'`); err != nil {
		t.Fatal(err)
	}
	keysMgr.ReloadGroupCache()
	gk, _ = keysMgr.ValidateClientKey(groupKey.RawKey)
	if !gk.IsModelAllowed("p1/gone") {
		t.Error("model must be allowed again once it is no longer removed")
	}
}
