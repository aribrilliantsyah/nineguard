package models

import (
	"reflect"
	"testing"
)

func TestRemovedSummary(t *testing.T) {
	mgr, database := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c", "d", "e"))
	mgr.applyListing(listing("p1", "a", "e")) // 3 of 5 missing: guard holds
	mgr.applyListing(listing("p1", "a", "e")) // confirmed: b, c, d removed

	for _, q := range []string{
		`INSERT INTO api_keys (id, key, prefix, name, model_access_mode, model_group_ids) VALUES ('k1', 'sk-ng-1111', 'sk-ng-', 'one', 'group', '["G1"]')`,
		`INSERT INTO api_keys (id, key, prefix, name, model_access_mode, model_group_ids) VALUES ('k2', 'sk-ng-2222', 'sk-ng-', 'two', 'group', '["G1","G2"]')`,
		`INSERT INTO api_keys (id, key, prefix, name, model_access_mode, model_group_ids) VALUES ('k3', 'sk-ng-3333', 'sk-ng-', 'three', 'group', '["G3"]')`,
		`INSERT INTO api_keys (id, key, prefix, name, model_access_mode, model_group_ids) VALUES ('k4', 'sk-ng-4444', 'sk-ng-', 'four', 'all', '[]')`,
		`INSERT INTO model_groups (id, name, description, models) VALUES ('G1', 'dead', '', '["p1/b","p1/a"]')`,
		`INSERT INTO model_groups (id, name, description, models) VALUES ('G2', 'wild', '', '["p1/*"]')`,
		`INSERT INTO model_groups (id, name, description, models) VALUES ('G3', 'clean', '', '["p1/a"]')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	s, err := mgr.GetRemovedSummary()
	if err != nil {
		t.Fatal(err)
	}
	if s.RemovedCount != 3 {
		t.Errorf("removed_count = %d, want 3", s.RemovedCount)
	}
	if len(s.Providers) != 1 || s.Providers[0].ProviderID != "p1" || s.Providers[0].Count != 3 {
		t.Fatalf("providers = %+v", s.Providers)
	}
	if !reflect.DeepEqual(s.Providers[0].Sample, []string{"p1/b", "p1/c", "p1/d"}) {
		t.Errorf("sample = %v", s.Providers[0].Sample)
	}
	if len(s.Groups) != 1 || s.Groups[0].ID != "G1" || s.UnavailableCount != 1 {
		t.Fatalf("groups = %+v unavailable=%d", s.Groups, s.UnavailableCount)
	}
	// k1 and k2 link G1 (k2 also links the clean wildcard group). k3 and k4 are unaffected.
	if s.AffectedKeys != 2 {
		t.Errorf("affected_keys = %d, want 2", s.AffectedKeys)
	}
}

func TestRemovedSummaryEmptyIsNotNil(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	s, err := mgr.GetRemovedSummary()
	if err != nil {
		t.Fatal(err)
	}
	if s.RemovedCount != 0 || s.Providers == nil || s.Groups == nil || s.Sync.PendingProviders == nil {
		t.Fatalf("empty summary must serialise as [] not null: %+v", s)
	}
	if s.Sync.LastSuccessAt != nil {
		t.Error("no sync has run yet: last_success_at must be absent")
	}
}

func TestSyncStatusReportsPendingGuard(t *testing.T) {
	mgr, _ := newSyncTestManager(t)
	mgr.applyListing(listing("p1", "a", "b", "c", "d"))

	mgr.applyListing(listing("p1", "a")) // guard holds
	if got := mgr.GetSyncStatus().PendingProviders; !reflect.DeepEqual(got, []string{"p1"}) {
		t.Fatalf("pending = %v, want [p1]", got)
	}
	mgr.applyListing(listing("p1", "a")) // confirmed
	if got := mgr.GetSyncStatus().PendingProviders; len(got) != 0 {
		t.Fatalf("pending should clear after confirmation, got %v", got)
	}
}
