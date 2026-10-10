package models

import (
	"fmt"
	"path/filepath"
	"testing"

	"nineguard/internal/db"
)

func newSyncTestManager(t *testing.T) (*Manager, *db.DB) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO providers (id, name, prefix, route, is_active) VALUES ('p1', 'P1', 'p1', 'http://x', 1)`); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return NewManager(database), database
}

func listing(provider string, names ...string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]interface{}{"id": provider + "/" + n, "provider": provider})
	}
	return out
}

func removedAt(t *testing.T, mgr *Manager, id string) (found, removed bool) {
	t.Helper()
	list, err := mgr.ListModels("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, m := range list {
		if m.ID == id {
			return true, m.Removed
		}
	}
	return false, false
}

func TestSyncSoftDeletesAndClears(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c"))

	// b disappears upstream: 1 of 3 missing, under the 50% guard.
	_, removed := mgr.applyListing(listing("p1", "a", "c"))
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	found, isRemoved := removedAt(t, mgr, "p1/b")
	if !found || !isRemoved {
		t.Fatalf("p1/b should stay as removed, found=%v removed=%v", found, isRemoved)
	}

	// b returns: flag clears.
	mgr.applyListing(listing("p1", "a", "b", "c"))
	if _, isRemoved := removedAt(t, mgr, "p1/b"); isRemoved {
		t.Fatal("p1/b should be present again")
	}
}

func TestSyncEmptyListingMarksNothing(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b"))

	// Provider returned nothing (or errored): it is absent from the listing.
	mgr.applyListing(nil)
	for _, id := range []string{"p1/a", "p1/b"} {
		if found, isRemoved := removedAt(t, mgr, id); !found || isRemoved {
			t.Fatalf("%s must be untouched, found=%v removed=%v", id, found, isRemoved)
		}
	}
}

func TestSyncMassRemovalNeedsSecondSync(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c", "d"))

	// 3 of 4 missing: over 50%, first sync holds back.
	_, removed := mgr.applyListing(listing("p1", "a"))
	if removed != 0 {
		t.Fatalf("first sync must mark none, got %d", removed)
	}
	if _, isRemoved := removedAt(t, mgr, "p1/b"); isRemoved {
		t.Fatal("p1/b must not be removed after one sync")
	}

	// Second consecutive sync agrees: applied.
	_, removed = mgr.applyListing(listing("p1", "a"))
	if removed != 3 {
		t.Fatalf("second sync must mark 3, got %d", removed)
	}
	if _, isRemoved := removedAt(t, mgr, "p1/d"); !isRemoved {
		t.Fatal("p1/d should be removed after confirmation")
	}
}

func TestSyncMassRemovalPendingResetsWhenListingRecovers(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c", "d"))

	mgr.applyListing(listing("p1", "a")) // pending
	mgr.applyListing(listing("p1", "a", "b", "c", "d")) // full listing back: pending clears
	_, removed := mgr.applyListing(listing("p1", "a")) // pending again, not applied
	if removed != 0 {
		t.Fatalf("pending must restart, got %d removed", removed)
	}
}

func TestSyncKeepsModelsOfInactiveProvider(t *testing.T) {
	mgr, database := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b"))

	if _, err := database.Exec(`UPDATE providers SET is_active = 0 WHERE id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	mgr.applyListing(nil)
	for _, id := range []string{"p1/a", "p1/b"} {
		if found, isRemoved := removedAt(t, mgr, id); !found || isRemoved {
			t.Fatalf("%s must survive deactivation untouched, found=%v removed=%v", id, found, isRemoved)
		}
	}
}

func TestSyncDeletesOrphans(t *testing.T) {
	mgr, database := newSyncTestManager(t)
	if _, err := database.Exec(`INSERT INTO models (id, name, provider_id, enabled) VALUES ('ghost/x', 'ghost/x', 'ghost', 1)`); err != nil {
		t.Fatal(err)
	}
	mgr.applyListing(nil)
	if found, _ := removedAt(t, mgr, "ghost/x"); found {
		t.Fatal("model of a deleted provider should be hard-deleted")
	}
}

func TestSetModelEnabledKeepsRemovedFlag(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c"))
	mgr.applyListing(listing("p1", "a", "c"))

	if err := mgr.SetModelEnabled("p1/b", true); err != nil {
		t.Fatal(err)
	}
	if _, isRemoved := removedAt(t, mgr, "p1/b"); !isRemoved {
		t.Fatal(fmt.Sprint("re-enabling must not clear removed_at"))
	}
}
