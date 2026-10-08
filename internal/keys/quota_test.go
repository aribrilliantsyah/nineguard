package keys_test

import (
	"testing"
	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func TestKeyQuotaCRUD(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := keys.NewManager(d, "")

	// Create key with quota
	info, err := mgr.CreateKeyWithOptions("test-quota", keys.CreateKeyOptions{
		QuotaLimit:  50000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKeyWithOptions: %v", err)
	}
	if info.QuotaLimit != 50000 || info.QuotaPeriod != "daily" {
		t.Errorf("created key mismatch: %+v", info)
	}

	// Get key
	got, err := mgr.GetKey(info.ID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if got.QuotaLimit != 50000 || got.QuotaPeriod != "daily" {
		t.Errorf("got key mismatch: %+v", got)
	}

	// Update quota
	gotAfter, err := mgr.UpdateKeyQuota(info.ID, 100000, "monthly")
	if err != nil {
		t.Fatalf("UpdateKeyQuota: %v", err)
	}
	if gotAfter.QuotaLimit != 100000 || gotAfter.QuotaPeriod != "monthly" {
		t.Errorf("updated key mismatch: %+v", gotAfter)
	}

	// Also check ListKeys
	list, err := mgr.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	found := false
	for _, k := range list {
		if k.ID == info.ID {
			found = true
			if k.QuotaLimit != 100000 || k.QuotaPeriod != "monthly" {
				t.Errorf("list key quota mismatch: %+v", k)
			}
		}
	}
	if !found {
		t.Errorf("key not found in ListKeys")
	}

	// Also check ListKeysPage
	page, err := mgr.ListKeysPage(keys.ListOptions{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("ListKeysPage: %v", err)
	}
	foundInPage := false
	for _, k := range page.Keys {
		if k.ID == info.ID {
			foundInPage = true
			if k.QuotaLimit != 100000 || k.QuotaPeriod != "monthly" {
				t.Errorf("page key quota mismatch: %+v", k)
			}
		}
	}
	if !foundInPage {
		t.Errorf("key not found in ListKeysPage")
	}

	// Insert traffic: 15k today, 25k two days ago
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 15000, datetime('now'))`, info.ID)
	if err != nil {
		t.Fatalf("insert traffic today: %v", err)
	}
	_, err = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 25000, datetime('now', '-2 days'))`, info.ID)
	if err != nil {
		t.Fatalf("insert traffic yesterday: %v", err)
	}

	// Switch to daily quota: QuotaUsage should be 15000, TotalTokens should be 40000
	_, err = mgr.UpdateKeyQuota(info.ID, 50000, "daily")
	if err != nil {
		t.Fatalf("UpdateKeyQuota daily: %v", err)
	}

	listWithTraffic, err := mgr.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys with traffic: %v", err)
	}
	for _, k := range listWithTraffic {
		if k.ID == info.ID {
			if k.TotalTokens != 40000 {
				t.Errorf("expected lifetime TotalTokens=40000, got %d", k.TotalTokens)
			}
			if k.QuotaUsage != 15000 {
				t.Errorf("expected daily QuotaUsage=15000, got %d", k.QuotaUsage)
			}
		}
	}
}
