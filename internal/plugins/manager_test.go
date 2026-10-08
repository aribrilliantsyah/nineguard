package plugins

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
)

func TestManager_CRUDAndCache(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tmpDir, "plugins_mgr.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	mgr := NewManager(database, nil)
	plugins, err := mgr.ListPlugins()
	if err != nil || len(plugins) < 3 {
		t.Fatalf("expected at least 3 seeded plugins, got %d (err: %v)", len(plugins), err)
	}

	// Secret masking check
	for _, p := range plugins {
		if p.Secret != "" {
			t.Fatalf("plugin secret should be empty/masked on list")
		}
	}

	// Create HTTP plugin
	newP := &Plugin{
		Name:          "PII Guard",
		Description:   "Anonymizes PII",
		URL:           "http://localhost:9090",
		FailurePolicy: PolicyClosed,
		Category:      CategoryOther,
	}
	created, rawSecret, err := mgr.CreateHTTPPlugin(newP)
	if err != nil {
		t.Fatalf("failed to create http plugin: %v", err)
	}
	if rawSecret == "" {
		t.Fatalf("expected raw secret returned on create")
	}
	if created.Kind != KindHTTP {
		t.Errorf("expected KindHTTP, got %s", created.Kind)
	}

	// Fetch created plugin: secret should be masked
	fetched, err := mgr.GetPlugin(created.ID)
	if err != nil || fetched.Secret != "" {
		t.Fatalf("expected masked secret on get, got %q (err: %v)", fetched.Secret, err)
	}

	// Rotate secret
	newSecret, err := mgr.RotateSecret(created.ID)
	if err != nil || newSecret == "" || newSecret == rawSecret {
		t.Fatalf("expected new secret on rotate, got %q (err: %v)", newSecret, err)
	}

	// Binding upsert and retrieval
	b := Binding{
		PluginID:  created.ID,
		ScopeType: ScopeGroup,
		ScopeID:   "grp_1",
		State:     StateOn,
		Settings:  `{"threshold":0.8}`,
	}
	if err := mgr.UpsertBinding(b); err != nil {
		t.Fatalf("failed to upsert binding: %v", err)
	}

	bindings, err := mgr.ListBindings(created.ID)
	if err != nil || len(bindings) < 2 { // global off + grp_1 on
		t.Fatalf("expected at least 2 bindings, got %d (err: %v)", len(bindings), err)
	}

	// Delete HTTP plugin
	if err := mgr.DeletePlugin(created.ID); err != nil {
		t.Fatalf("failed to delete http plugin: %v", err)
	}

	// Builtin deletion rejection
	if err := mgr.DeletePlugin("caveman"); err == nil {
		t.Fatalf("expected error deleting built-in plugin, got nil")
	}
}

func TestManager_UpdatePipelineOrder(t *testing.T) {
	tmpDir := t.TempDir()
	database, _ := db.InitDB(filepath.Join(tmpDir, "order.db"))
	defer database.Close()

	mgr := NewManager(database, nil)
	newOrder := []string{"caveman", "headroom", "ponytail"}
	if err := mgr.UpdatePipelineOrder(newOrder); err != nil {
		t.Fatalf("failed to update order: %v", err)
	}

	list, _ := mgr.ListPlugins()
	orderMap := make(map[string]int)
	for _, p := range list {
		orderMap[p.ID] = p.PipelineOrder
	}

	if orderMap["caveman"] >= orderMap["headroom"] || orderMap["headroom"] >= orderMap["ponytail"] {
		t.Fatalf("unexpected order map: %v", orderMap)
	}
}
