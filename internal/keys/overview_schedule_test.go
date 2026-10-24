package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/keys"
)

// Model-mode keys have no key_quota object, so the overview carries the schedule on the key
// itself; the Quotas page needs it to show each Model Quota's reset in the right timezone.
func TestOverviewKeyCarriesEffectiveSchedule(t *testing.T) {
	mgr, d := newMgr(t)
	if _, err := d.Exec(`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/a','p/a','p',1)`); err != nil {
		t.Fatal(err)
	}
	model, _ := mgr.CreateKeyWithOptions("model", keys.CreateKeyOptions{
		ModelAccessMode: "custom", AllowedModels: []string{"p/a"}, QuotaMode: "model",
		ModelQuotas:   []keys.ModelQuota{{ModelID: "p/a", QuotaLimit: 100, QuotaPeriod: "daily"}},
		ResetSchedule: jkt("14:00", 1, 1),
	})
	follower, _ := mgr.CreateKeyWithOptions("follower", keys.CreateKeyOptions{QuotaLimit: 100, QuotaPeriod: "daily"})
	if _, err := mgr.SetGlobalSchedule(jkt("06:00", 1, 1)); err != nil {
		t.Fatal(err)
	}

	ov, err := mgr.QuotaOverview(80, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, k := range ov.Keys {
		switch k.ID {
		case model.ID:
			seen++
			if k.EffectiveResetSchedule == nil || k.EffectiveResetSchedule.Time != "14:00" || k.ResetScheduleSource != "key" {
				t.Errorf("model key: %+v source=%s", k.EffectiveResetSchedule, k.ResetScheduleSource)
			}
		case follower.ID:
			seen++
			if k.EffectiveResetSchedule == nil || k.EffectiveResetSchedule.Time != "06:00" || k.ResetScheduleSource != "global" {
				t.Errorf("follower: %+v source=%s", k.EffectiveResetSchedule, k.ResetScheduleSource)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("keys missing from overview, saw %d", seen)
	}
}

func TestOverviewKeyWithoutScheduleIsDefault(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("plain", keys.CreateKeyOptions{QuotaLimit: 100, QuotaPeriod: "daily"})
	ov, _ := mgr.QuotaOverview(80, time.Now())
	for _, ok := range ov.Keys {
		if ok.ID == k.ID {
			if ok.EffectiveResetSchedule != nil || ok.ResetScheduleSource != "default" {
				t.Errorf("default key: %+v source=%s", ok.EffectiveResetSchedule, ok.ResetScheduleSource)
			}
			return
		}
	}
	t.Fatal("key missing")
}
