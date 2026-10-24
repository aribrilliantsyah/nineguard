package keys_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func TestKeyCostVisibility(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := keys.NewManager(database, "")

	// 1. Create key with cost_visibility = "show"
	showVal := "show"
	k1, err := mgr.CreateKeyWithOptions("Key Show", keys.CreateKeyOptions{
		CostVisibility: &showVal,
	})
	if err != nil {
		t.Fatalf("CreateKeyWithOptions failed: %v", err)
	}
	if k1.CostVisibility == nil || *k1.CostVisibility != "show" {
		t.Fatalf("expected cost_visibility 'show', got %v", k1.CostVisibility)
	}

	// Read back from DB
	read1, err := mgr.GetKey(k1.ID)
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}
	if read1.CostVisibility == nil || *read1.CostVisibility != "show" {
		t.Fatalf("expected cost_visibility 'show' on read, got %v", read1.CostVisibility)
	}

	// 2. Update key to "hide"
	hideVal := "hide"
	upd, err := mgr.UpdateKeyAll(k1.ID, keys.KeyUpdate{
		Name:           k1.Name,
		CostVisibility: &hideVal,
	})
	if err != nil {
		t.Fatalf("UpdateKeyAll failed: %v", err)
	}
	if upd.CostVisibility == nil || *upd.CostVisibility != "hide" {
		t.Fatalf("expected cost_visibility 'hide' after update, got %v", upd.CostVisibility)
	}

	// 3. Clear cost_visibility (reset to null)
	emptyVal := ""
	cleared, err := mgr.UpdateKeyAll(k1.ID, keys.KeyUpdate{
		Name:           k1.Name,
		CostVisibility: &emptyVal,
	})
	if err != nil {
		t.Fatalf("UpdateKeyAll clear failed: %v", err)
	}
	if cleared.CostVisibility != nil {
		t.Fatalf("expected cost_visibility nil after clear, got %v", *cleared.CostVisibility)
	}
}
