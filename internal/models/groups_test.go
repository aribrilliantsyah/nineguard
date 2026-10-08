package models_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
)

func TestModelGroupsCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_models.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	mgr := models.NewManager(database)
	keysMgr := keys.NewManager(database, "")

	// 1. Initial list should be empty (no default data seeded)
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("failed to list initial groups: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 default seeded groups, got %d", len(groups))
	}

	// 2. Create custom group
	newGrp, err := mgr.CreateGroup("Coding Suite", "Fast models for code completions", []string{"gpt-4o", "claude-3-5-sonnet"})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if newGrp.Name != "Coding Suite" {
		t.Errorf("unexpected group name: %s", newGrp.Name)
	}
	if len(newGrp.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(newGrp.Models))
	}

	// 3. Get group
	fetched, err := mgr.GetGroup(newGrp.ID)
	if err != nil {
		t.Fatalf("failed to fetch group: %v", err)
	}
	if fetched.ID != newGrp.ID {
		t.Errorf("expected ID %s, got %s", newGrp.ID, fetched.ID)
	}

	// 4. Link an API key to this group
	_, err = keysMgr.CreateKey("Dev Key", "group", []string{newGrp.ID}, nil)
	if err != nil {
		t.Fatalf("failed to create key linked to group: %v", err)
	}

	// Verify key count in group
	fetchedAfterLink, err := mgr.GetGroup(newGrp.ID)
	if err != nil {
		t.Fatalf("failed to fetch group after link: %v", err)
	}
	if fetchedAfterLink.KeysCount != 1 {
		t.Errorf("expected KeysCount = 1, got %d", fetchedAfterLink.KeysCount)
	}

	// 5. Delete protection: should fail because key is linked
	err = mgr.DeleteGroup(newGrp.ID)
	if err == nil {
		t.Errorf("expected error deleting group with linked keys, got nil")
	}

	// 6. Update group models
	updated, err := mgr.UpdateGroup(newGrp.ID, "Coding Suite Pro", "Updated description", []string{"gpt-4o", "claude-3-5-sonnet", "o3-mini"})
	if err != nil {
		t.Fatalf("failed to update group: %v", err)
	}
	if updated.Name != "Coding Suite Pro" || len(updated.Models) != 3 {
		t.Errorf("unexpected updated group: %+v", updated)
	}
}

func TestGroupPriority(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_models_priority.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	mgr := models.NewManager(database)

	grp, err := mgr.CreateGroupWithPriority("HighPri", "desc", []string{"gpt-4"}, 10)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if grp.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", grp.Priority)
	}

	got, err := mgr.GetGroup(grp.ID)
	if err != nil || got.Priority != 10 {
		t.Fatalf("expected retrieved priority 10, got %d (err: %v)", got.Priority, err)
	}

	updated, err := mgr.UpdateGroupWithPriority(grp.ID, "HighPri Updated", "desc2", []string{"gpt-4"}, 20)
	if err != nil || updated.Priority != 20 {
		t.Fatalf("expected updated priority 20, got %d (err: %v)", updated.Priority, err)
	}
}

