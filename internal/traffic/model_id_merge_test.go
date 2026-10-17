package traffic

import "testing"

// Bare and prefixed requests for one model must show as one model everywhere
// (docs/BUGS_2026-10-16.md, bug 1: "yang udah tanggung otomatis disamakan").
func seedMergeFixtures(t *testing.T, mgr *Manager) {
	t.Helper()
	if _, err := mgr.db.Exec(`
		INSERT INTO models (id, name, provider_id, enabled) VALUES ('9dak/cx/x', '9dak/cx/x', '9dak', 1);
		INSERT INTO traffic_logs (api_key, client_ip, stream, model, model_id, status_code, total_tokens) VALUES ('k', '1.1.1.1', 0, 'cx/x', '9dak/cx/x', 200, 10);
		INSERT INTO traffic_logs (api_key, client_ip, stream, model, model_id, status_code, total_tokens) VALUES ('k', '1.1.1.1', 0, '9dak/cx/x', '9dak/cx/x', 200, 20);
		INSERT INTO traffic_logs (api_key, client_ip, stream, model, status_code, total_tokens) VALUES ('k', '1.1.1.1', 0, '9dak/cx/x', 200, 5);
		INSERT INTO traffic_logs (api_key, client_ip, stream, model, model_id, status_code, total_tokens) VALUES ('k', '1.1.1.1', 0, 'cx/x', '9dak/cx/x', 502, 0);
	`); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardMergesBareAndPrefixedModel(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	seedMergeFixtures(t, mgr)

	st, err := mgr.GetDashboardStats("all", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveModels != 1 {
		t.Errorf("distinct models: want 1, got %d", st.ActiveModels)
	}
	if len(st.TopModels) != 1 || st.TopModels[0].Model != "9dak/cx/x" || st.TopModels[0].Requests != 4 {
		t.Errorf("top models: want one row 9dak/cx/x with 4 requests, got %+v", st.TopModels)
	}
	if len(st.TopErrorSources) != 1 || st.TopErrorSources[0].Source != "9dak/cx/x" {
		t.Errorf("error sources: want 9dak/cx/x, got %+v", st.TopErrorSources)
	}
}

func TestUsageReportMergesBareAndPrefixedModel(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	seedMergeFixtures(t, mgr)

	rep, err := mgr.GetUsageReports("all", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ModelsBreakdown) != 1 {
		t.Fatalf("want one model row, got %+v", rep.ModelsBreakdown)
	}
	mb := rep.ModelsBreakdown[0]
	if mb.Model != "9dak/cx/x" || mb.TotalRequests != 4 || mb.TotalTokens != 35 {
		t.Errorf("want 9dak/cx/x with 4 requests / 35 tokens, got %+v", mb)
	}
}

func TestTrafficModelFilterMatchesBareAndPrefixed(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	seedMergeFixtures(t, mgr)

	logs, total, err := mgr.QueryLogs(FilterParams{Period: "all", Model: "9dak/cx/x", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(logs) != 4 {
		t.Errorf("filter by canonical id: want 4 rows (bare + prefixed + old), got total=%d len=%d", total, len(logs))
	}
}
