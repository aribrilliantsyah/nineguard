package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func insertUsage(t *testing.T, d *db.DB, keyID, modelID string, tokens int, at time.Time) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, model_id, status_code, total_tokens, timestamp) VALUES (?, ?, ?, 200, ?, ?)`,
		keyID, modelID, modelID, tokens, at.UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
}

// Key Quota usage and reset in GetKey, ListKeys and QuotaOverview all use the key's schedule.
func TestKeyQuotaUsesEffectiveScheduleEverywhere(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")
	// daily reset 14:00 Jakarta = 07:00 UTC
	k, err := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "daily", ResetSchedule: jkt("14:00", 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// the current window started at the most recent 07:00 UTC
	start := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC)
	if start.After(now) {
		start = start.AddDate(0, 0, -1)
	}
	insertUsage(t, d, k.ID, "", 700, start.Add(time.Second))
	insertUsage(t, d, k.ID, "", 5000, start.Add(-time.Hour)) // previous window

	got, _ := mgr.GetKey(k.ID)
	if got.QuotaUsage != 700 {
		t.Errorf("GetKey usage = %d, want 700", got.QuotaUsage)
	}
	list, _ := mgr.ListKeys()
	for _, ki := range list {
		if ki.ID == k.ID && ki.QuotaUsage != 700 {
			t.Errorf("ListKeys usage = %d, want 700", ki.QuotaUsage)
		}
	}
	ov, _ := mgr.QuotaOverview(80, now)
	found := false
	for _, ok := range ov.Keys {
		if ok.ID != k.ID {
			continue
		}
		found = true
		if ok.KeyQuota == nil || ok.KeyQuota.Usage != 700 {
			t.Fatalf("overview usage: %+v", ok.KeyQuota)
		}
		if ok.KeyQuota.ResetAt == nil || *ok.KeyQuota.ResetAt != start.AddDate(0, 0, 1).Format(time.RFC3339) {
			t.Errorf("overview reset_at = %v, want %s", ok.KeyQuota.ResetAt, start.AddDate(0, 0, 1).Format(time.RFC3339))
		}
	}
	if !found {
		t.Fatal("key missing from overview")
	}
}

func TestModelQuotasUseEffectiveScheduleAndPin(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/a','p/a','p',1)`); err != nil {
		t.Fatal(err)
	}
	mgr := keys.NewManager(d, "")
	k, err := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{
		ModelAccessMode: "custom", AllowedModels: []string{"p/a"}, QuotaMode: "model",
		ModelQuotas: []keys.ModelQuota{{ModelID: "p/a", QuotaLimit: 5000, QuotaPeriod: "monthly"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	insertUsage(t, d, k.ID, "p/a", 4100, firstOfMonth.Add(time.Minute))

	before, _ := mgr.GetKey(k.ID)
	if before.ModelQuotas[0].Usage != 4100 {
		t.Fatalf("usage before = %d", before.ModelQuotas[0].Usage)
	}

	// Move the monthly reset to a day that is not the 1st. Usage must not move.
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "custom", AllowedModels: []string{"p/a"},
		ResetSchedule: &keys.ScheduleChange{Value: jkt("00:00", 1, 20)}}); err != nil {
		t.Fatal(err)
	}
	after, _ := mgr.GetKey(k.ID)
	if after.ModelQuotas[0].Usage != 4100 {
		t.Errorf("usage changed by schedule edit: %d -> %d", before.ModelQuotas[0].Usage, after.ModelQuotas[0].Usage)
	}
	pin, ok := after.ResetRule.Pins["monthly"]
	if !ok {
		t.Fatalf("monthly pin missing: %+v", after.ResetRule.Pins)
	}
	if after.ModelQuotas[0].ResetAt == nil || *after.ModelQuotas[0].ResetAt != pin.Until.UTC().Format(time.RFC3339) {
		t.Errorf("reset_at = %v, want pin Until %s", after.ModelQuotas[0].ResetAt, pin.Until.UTC().Format(time.RFC3339))
	}
}

// A key with no schedule anywhere behaves exactly as before.
func TestNoScheduleKeepsUTCCalendar(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")
	k, _ := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "monthly"})
	ov, _ := mgr.QuotaOverview(80, time.Now())
	for _, ok := range ov.Keys {
		if ok.ID == k.ID {
			_, want := traffic.CalcQuotaWindow("monthly", traffic.ResetRule{}, time.Now())
			if ok.KeyQuota == nil || ok.KeyQuota.ResetAt == nil || *ok.KeyQuota.ResetAt != want.UTC().Format(time.RFC3339) {
				t.Errorf("reset_at = %v, want %s", ok.KeyQuota, want.UTC().Format(time.RFC3339))
			}
		}
	}
}
