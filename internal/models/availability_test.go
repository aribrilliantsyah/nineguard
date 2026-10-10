package models

import (
	"reflect"
	"testing"
)

func seedRemoved(t *testing.T) *Manager {
	t.Helper()
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c"))
	mgr.applyListing(listing("p1", "a", "c")) // p1/b removed
	return mgr
}

func TestGroupReportsUnavailableEntries(t *testing.T) {
	mgr := seedRemoved(t)
	g, err := mgr.CreateGroup("G", "", []string{"p1/a", "p1/b", "b", "p1/*", "all", "unknown/model"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := mgr.GetGroup(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Full id and bare name both name the Removed Model. Wildcard, "all" and unknown names never flag.
	want := []string{"p1/b", "b"}
	if !reflect.DeepEqual(got.UnavailableModels, want) {
		t.Fatalf("unavailable = %v, want %v", got.UnavailableModels, want)
	}
	if got.UnavailableCount != 2 || got.ModelsCount != 6 {
		t.Fatalf("counts: unavailable=%d total=%d, want 2 and 6", got.UnavailableCount, got.ModelsCount)
	}

	list, _ := mgr.ListGroups()
	if len(list) != 1 || list[0].UnavailableCount != 2 {
		t.Fatalf("ListGroups unavailable_count = %+v", list)
	}
}

func TestBareNameStaysAvailableWhenAnotherProviderServesIt(t *testing.T) {
	mgr, database := newSyncTestManager(t)
	if _, err := database.Exec(`INSERT INTO providers (id, name, prefix, route, is_active) VALUES ('p2', 'P2', 'p2', 'http://y', 1)`); err != nil {
		t.Fatal(err)
	}
	mgr.applyListing(append(listing("p1", "shared", "x", "y"), listing("p2", "shared")...))
	mgr.applyListing(append(listing("p1", "x", "y"), listing("p2", "shared")...)) // p1/shared removed

	g, _ := mgr.CreateGroup("G", "", []string{"shared", "p1/shared"})
	got, _ := mgr.GetGroup(g.ID)
	if !reflect.DeepEqual(got.UnavailableModels, []string{"p1/shared"}) {
		t.Fatalf("only the full removed id should flag, got %v", got.UnavailableModels)
	}
}

func TestRemoveUnavailableEntries(t *testing.T) {
	mgr := seedRemoved(t)
	g, _ := mgr.CreateGroup("G", "d", []string{"p1/a", "p1/b", "p1/*"})

	updated, dropped, err := mgr.RemoveUnavailableEntries(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if !reflect.DeepEqual(updated.Models, []string{"p1/a", "p1/*"}) {
		t.Fatalf("models = %v", updated.Models)
	}
	if updated.UnavailableCount != 0 || updated.Name != "G" || updated.Description != "d" {
		t.Fatalf("group metadata changed: %+v", updated)
	}

	// Nothing left to drop: no-op.
	_, dropped, err = mgr.RemoveUnavailableEntries(g.ID)
	if err != nil || dropped != 0 {
		t.Fatalf("second call dropped=%d err=%v", dropped, err)
	}

	if _, _, err := mgr.RemoveUnavailableEntries("grp_missing"); err == nil {
		t.Fatal("unknown group must error")
	}
}
