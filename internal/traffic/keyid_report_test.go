package traffic

import (
	"testing"
)

func insertKeyed(t *testing.T, mgr *Manager, ts, keyID, keyName, masked, model string, status, tokens int) {
	t.Helper()
	var id any
	if keyID != "" {
		id = keyID
	}
	_, err := mgr.db.Exec(`
		INSERT INTO traffic_logs (timestamp, api_key, api_key_name, api_key_id, model, total_tokens, status_code, client_ip, level)
		VALUES (?, ?, ?, ?, ?, ?, ?, '127.0.0.1', '')`, ts, masked, keyName, id, model, tokens, status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUsageReportGroupsByKeyID(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")

	if _, err := mgr.db.Exec(`INSERT INTO api_keys (id, key, prefix, name) VALUES
		('k-pi', 'sk-ng-aaaa1111', 'sk-ng-', 'pi-dev-renamed'),
		('k-twin', 'sk-ng-bbbb2222', 'sk-ng-', 'twin')`); err != nil {
		t.Fatal(err)
	}
	// Linked: old name in traffic, current name from api_keys.
	insertKeyed(t, mgr, "2026-10-06 09:00:00", "k-pi", "pi-dev", "sk-ng-...1111", "m1", 200, 100)
	insertKeyed(t, mgr, "2026-10-06 09:30:00", "k-pi", "pi-dev-renamed", "sk-ng-...1111", "m2", 200, 50)
	// Same historical name, different linked key: must stay separate.
	insertKeyed(t, mgr, "2026-10-06 08:00:00", "k-twin", "pi-dev", "sk-ng-...2222", "m1", 200, 10)
	// Unlinked (NULL) and deleted-key rows group by historical name.
	insertKeyed(t, mgr, "2026-10-06 07:00:00", "", "legacy-bot", "sk-ng-...9999", "m1", 200, 7)
	insertKeyed(t, mgr, "2026-10-06 07:30:00", "k-deleted", "legacy-bot", "sk-ng-...8888", "m1", 403, 0)

	report, err := mgr.GetUsageReports("today", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]KeyUsageBreakdown{}
	for _, k := range report.KeysBreakdown {
		byName[k.KeyName] = k
	}
	if len(report.KeysBreakdown) != 3 {
		t.Fatalf("groups = %d, want 3: %+v", len(report.KeysBreakdown), report.KeysBreakdown)
	}
	pi := byName["pi-dev-renamed"]
	if pi.KeyID != "k-pi" || pi.Unlinked || pi.TotalRequests != 2 || pi.TotalTokens != 150 || len(pi.ModelUsage) != 2 {
		t.Errorf("pi group = %+v", pi)
	}
	if pi.LastActiveAt == nil || *pi.LastActiveAt != "2026-10-06T09:30:00Z" {
		t.Errorf("pi last active = %v", pi.LastActiveAt)
	}
	twin := byName["twin"]
	if twin.KeyID != "k-twin" || twin.TotalRequests != 1 {
		t.Errorf("twin group = %+v", twin)
	}
	legacy := byName["legacy-bot"]
	if !legacy.Unlinked || legacy.KeyID != "" || legacy.TotalRequests != 2 || legacy.BlockedRequests != 1 {
		t.Errorf("unlinked group = %+v", legacy)
	}
	if legacy.LastActiveAt == nil || *legacy.LastActiveAt != "2026-10-06T07:30:00Z" {
		t.Errorf("unlinked last active = %v", legacy.LastActiveAt)
	}

	// Model consumers carry key IDs.
	for _, mb := range report.ModelsBreakdown {
		if mb.Model != "m1" {
			continue
		}
		ids := map[string]bool{}
		for _, c := range mb.KeyConsumers {
			ids[c.KeyID] = true
			if c.KeyID == "" && !c.Unlinked {
				t.Errorf("consumer without ID must be unlinked: %+v", c)
			}
		}
		if !ids["k-pi"] || !ids["k-twin"] || !ids[""] {
			t.Errorf("m1 consumers = %+v", mb.KeyConsumers)
		}
	}

	stats, err := mgr.GetDashboardStats("today", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveKeys != 3 {
		t.Errorf("active keys = %d, want 3", stats.ActiveKeys)
	}
	if len(stats.TopKeys) != 3 || stats.TopKeys[0].KeyID != "k-pi" || stats.TopKeys[0].Name != "pi-dev-renamed" {
		t.Errorf("top keys = %+v", stats.TopKeys)
	}
	if stats.TopKeys[0].Trend == nil || sum(stats.TopKeys[0].Trend) != 2 {
		t.Errorf("pi trend = %v", stats.TopKeys[0].Trend)
	}
	found := false
	for _, p := range stats.KeyUsageTrends {
		if p.KeyID == "k-pi" && p.KeyName == "pi-dev-renamed" {
			found = true
		}
	}
	if !found {
		t.Errorf("key usage trends missing k-pi: %+v", stats.KeyUsageTrends)
	}

	// Traffic filter by key ID.
	logs, total, err := mgr.QueryLogs(FilterParams{Period: "all", APIKeyID: "k-pi"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || logs[0].APIKeyID != "k-pi" {
		t.Errorf("key_id filter: total %d", total)
	}
	vol, err := mgr.GetVolume(FilterParams{Period: "today", APIKeyID: "k-twin"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if vol.Totals["2xx"] != 1 {
		t.Errorf("volume key_id filter: %v", vol.Totals)
	}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}
