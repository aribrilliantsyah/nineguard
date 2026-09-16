package providers_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/providers"
)

func countDefaults(t *testing.T, mgr *providers.Manager) (int, *providers.Provider) {
	t.Helper()
	list, err := mgr.ListProviders()
	if err != nil {
		t.Fatalf("failed to list providers: %v", err)
	}

	count := 0
	var defaultProv *providers.Provider
	for _, p := range list {
		if p.IsDefault {
			count++
			pCopy := p
			defaultProv = &pCopy
		}
	}
	return count, defaultProv
}

func TestProvidersDefaultManagement(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	mgr := providers.NewManager(database)

	// 1. Create first provider with isDefault=false.
	// Since it's the first provider, it should automatically be set as default.
	p1, err := mgr.CreateProvider("Provider One", "http://localhost:11434", "key1", "ollama", false, true)
	if err != nil {
		t.Fatalf("failed to create p1: %v", err)
	}
	if !p1.IsDefault {
		t.Errorf("expected first provider to automatically become default")
	}

	defCount, defProv := countDefaults(t, mgr)
	if defCount != 1 || defProv == nil || defProv.ID != p1.ID {
		t.Fatalf("expected exactly 1 default provider (p1), got count=%d, prov=%v", defCount, defProv)
	}

	// 2. Create second provider with isDefault=false.
	// p1 should remain default, p2 should NOT be default.
	p2, err := mgr.CreateProvider("Provider Two", "https://api.openai.com", "key2", "openai", false, true)
	if err != nil {
		t.Fatalf("failed to create p2: %v", err)
	}
	if p2.IsDefault {
		t.Errorf("expected p2 not to be default")
	}

	defCount, defProv = countDefaults(t, mgr)
	if defCount != 1 || defProv.ID != p1.ID {
		t.Fatalf("expected default to remain p1, got count=%d, prov=%v", defCount, defProv)
	}

	// 3. Create third provider with isDefault=true.
	// Default MUST shift to p3, and MUST NOT be 2 defaults!
	p3, err := mgr.CreateProvider("Provider Three", "https://openrouter.ai/api", "key3", "openrouter", true, true)
	if err != nil {
		t.Fatalf("failed to create p3: %v", err)
	}
	if !p3.IsDefault {
		t.Errorf("expected p3 to be default")
	}

	defCount, defProv = countDefaults(t, mgr)
	if defCount != 1 {
		t.Fatalf("expected exactly 1 default provider (not 2!), got count=%d", defCount)
	}
	if defProv.ID != p3.ID {
		t.Fatalf("expected default to have shifted to p3, got id=%s", defProv.ID)
	}

	// Verify p1 is no longer default
	p1Check, err := mgr.GetProvider(p1.ID)
	if err != nil {
		t.Fatalf("failed to get p1: %v", err)
	}
	if p1Check.IsDefault {
		t.Errorf("expected p1 to no longer be default after p3 was created as default")
	}

	// 4. Update provider p2: set isDefault=true.
	// Must not deadlock, and default must shift to p2.
	updatedP2, err := mgr.UpdateProvider(p2.ID, "Provider Two Renamed", p2.Route, p2.APIKey, p2.Prefix, true, true)
	if err != nil {
		t.Fatalf("failed to update p2: %v", err)
	}
	if !updatedP2.IsDefault {
		t.Errorf("expected updated p2 to be default")
	}

	defCount, defProv = countDefaults(t, mgr)
	if defCount != 1 {
		t.Fatalf("expected exactly 1 default provider after update, got count=%d", defCount)
	}
	if defProv.ID != p2.ID {
		t.Fatalf("expected default to have shifted to p2, got id=%s", defProv.ID)
	}

	// Verify p3 is no longer default
	p3Check, err := mgr.GetProvider(p3.ID)
	if err != nil {
		t.Fatalf("failed to get p3: %v", err)
	}
	if p3Check.IsDefault {
		t.Errorf("expected p3 to no longer be default after p2 was set to default")
	}

	// 5. Test SetDefaultProvider explicitly
	if err := mgr.SetDefaultProvider(p1.ID); err != nil {
		t.Fatalf("failed to set p1 as default: %v", err)
	}

	defCount, defProv = countDefaults(t, mgr)
	if defCount != 1 || defProv.ID != p1.ID {
		t.Fatalf("expected exactly 1 default provider (p1), got count=%d, prov=%v", defCount, defProv)
	}

	// 6. Delete default provider (p1).
	// Another active provider should automatically be promoted to default so system has fallback.
	if err := mgr.DeleteProvider(p1.ID); err != nil {
		t.Fatalf("failed to delete p1: %v", err)
	}

	defCount, defProv = countDefaults(t, mgr)
	if defCount != 1 {
		t.Fatalf("expected exactly 1 default provider after deleting default provider, got count=%d", defCount)
	}
	if defProv.ID == p1.ID {
		t.Fatalf("deleted provider cannot be default")
	}
}
