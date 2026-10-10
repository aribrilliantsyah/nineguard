package traffic

import "testing"

// seedRemovalFixtures: one Removed Model, one live model, one model with no models row
// (e.g. hard-deleted before soft delete shipped).
func seedRemovalFixtures(t *testing.T, mgr *Manager) {
	t.Helper()
	if _, err := mgr.db.Exec(`
		INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p/gone', 'p/gone', 'p', 1, CURRENT_TIMESTAMP);
		INSERT INTO models (id, name, provider_id, enabled) VALUES ('p/live', 'p/live', 'p', 1);
	`); err != nil {
		t.Fatal(err)
	}
	insertKeyed(t, mgr, "2026-10-06 09:00:00", "", "bot", "sk-ng-...1111", "p/gone", 200, 10)
	insertKeyed(t, mgr, "2026-10-06 09:01:00", "", "bot", "sk-ng-...1111", "p/live", 200, 10)
	insertKeyed(t, mgr, "2026-10-06 09:02:00", "", "bot", "sk-ng-...1111", "old/forgotten", 200, 10)
}

func TestQueryLogsTagsRemovedModels(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	seedRemovalFixtures(t, mgr)

	logs, _, err := mgr.QueryLogs(FilterParams{Period: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, l := range logs {
		got[l.Model] = l.ModelRemoved
	}
	want := map[string]bool{"p/gone": true, "p/live": false, "old/forgotten": false}
	for model, removed := range want {
		if v, ok := got[model]; !ok || v != removed {
			t.Errorf("model %s: model_removed = %v (present=%v), want %v", model, v, ok, removed)
		}
	}
}

func TestQueryLogsTagDropsAfterModelDeleted(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	seedRemovalFixtures(t, mgr)

	if _, err := mgr.db.Exec(`DELETE FROM models WHERE id = 'p/gone'`); err != nil {
		t.Fatal(err)
	}
	logs, _, err := mgr.QueryLogs(FilterParams{Period: "all", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if l.ModelRemoved {
			t.Errorf("%s still tagged after the model row was deleted", l.Model)
		}
	}
}

func TestUsageReportTagsRemovedModels(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")
	seedRemovalFixtures(t, mgr)

	report, err := mgr.GetUsageReports("today", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range report.ModelsBreakdown {
		got[m.Model] = m.Removed
	}
	if len(got) != 3 {
		t.Fatalf("models in report = %v, want 3", got)
	}
	if !got["p/gone"] || got["p/live"] || got["old/forgotten"] {
		t.Errorf("removed flags wrong: %v", got)
	}
}
