package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func pinsOf(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.QueryRow(`SELECT COALESCE(quota_window_pins,'') FROM api_keys WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func ownOf(t *testing.T, d *db.DB, id string) string {
	t.Helper()
	var s string
	if err := d.QueryRow(`SELECT COALESCE(quota_reset_schedule,'') FROM api_keys WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func globalRow(t *testing.T, d *db.DB) (string, bool) {
	t.Helper()
	var v string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = 'quota_reset_schedule'`).Scan(&v)
	return v, err == nil
}

func TestSetGlobalScheduleMovesFollowersOnly(t *testing.T) {
	mgr, d := newMgr(t)
	follower, _ := mgr.CreateKeyWithOptions("follower", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	custom, _ := mgr.CreateKeyWithOptions("custom", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly", ResetSchedule: jkt("09:00", 2, 5)})

	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	insertUsage(t, d, follower.ID, "", 4100, firstOfMonth.Add(time.Minute))
	insertUsage(t, d, custom.ID, "", 4100, firstOfMonth.Add(time.Minute))
	beforeF, _ := mgr.GetKey(follower.ID)
	beforeC, _ := mgr.GetKey(custom.ID)
	customPinsBefore := pinsOf(t, d, custom.ID)

	counts, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20))
	if err != nil {
		t.Fatal(err)
	}

	f, _ := mgr.GetKey(follower.ID)
	if f.ResetScheduleSource != "global" || f.EffectiveResetSchedule == nil || f.EffectiveResetSchedule.Time != "14:00" || f.ResetSchedule != nil {
		t.Errorf("follower: source=%s eff=%+v", f.ResetScheduleSource, f.EffectiveResetSchedule)
	}
	if _, ok := f.ResetRule.Pins["monthly"]; !ok {
		t.Errorf("follower must get a monthly pin: %+v", f.ResetRule.Pins)
	}
	if f.QuotaUsage != beforeF.QuotaUsage || f.QuotaUsage != 4100 {
		t.Errorf("follower usage changed: %d -> %d", beforeF.QuotaUsage, f.QuotaUsage)
	}

	c, _ := mgr.GetKey(custom.ID)
	if c.ResetScheduleSource != "key" || c.EffectiveResetSchedule.Time != "09:00" {
		t.Errorf("custom key must keep its own schedule: source=%s eff=%+v", c.ResetScheduleSource, c.EffectiveResetSchedule)
	}
	if pinsOf(t, d, custom.ID) != customPinsBefore || c.QuotaUsage != beforeC.QuotaUsage {
		t.Errorf("custom key must be untouched: pins %q -> %q", customPinsBefore, pinsOf(t, d, custom.ID))
	}

	if v, ok := globalRow(t, d); !ok || v == "" {
		t.Errorf("settings row missing: %q", v)
	}
	if counts.Custom != 1 || counts.Following < 1 {
		t.Errorf("counts = %+v", counts)
	}

	// the proxy path (cache) sees it without a restart
	vk, ok := mgr.ValidateClientKey(follower.RawKey)
	if !ok || vk.EffectiveResetSchedule == nil || vk.EffectiveResetSchedule.Time != "14:00" || vk.ResetRule.Schedule == nil {
		t.Errorf("cache not refreshed: %+v", vk)
	}
}

func TestScheduleCountsIncludeInactiveKeys(t *testing.T) {
	mgr, d := newMgr(t)
	a, _ := mgr.CreateKey("a", "all", nil, nil)
	b, _ := mgr.CreateKeyWithOptions("b", keys.CreateKeyOptions{ResetSchedule: jkt("09:00", 1, 1)})
	c, _ := mgr.CreateKey("c", "all", nil, nil)
	if err := mgr.ToggleKey(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ToggleKey(b.ID, false); err != nil {
		t.Fatal(err)
	}
	_ = c

	var total int
	_ = d.QueryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&total)
	counts, err := mgr.ScheduleCounts()
	if err != nil {
		t.Fatal(err)
	}
	if counts.Custom != 1 || counts.Following != total-1 {
		t.Errorf("counts = %+v, total keys %d", counts, total)
	}
}

func TestRemovingGlobalScheduleReturnsFollowersToDefault(t *testing.T) {
	mgr, d := newMgr(t)
	follower, _ := mgr.CreateKeyWithOptions("follower", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	now := time.Now().UTC()
	insertUsage(t, d, follower.ID, "", 4100, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Add(time.Minute))

	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err != nil {
		t.Fatal(err)
	}
	// let the first transitional window go: drop its pins so removal is measured on its own
	if _, err := d.Exec(`UPDATE api_keys SET quota_window_pins = NULL`); err != nil {
		t.Fatal(err)
	}
	mgr.ReloadGlobalSchedule()
	before, _ := mgr.GetKey(follower.ID)

	if _, err := mgr.SetGlobalSchedule(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := globalRow(t, d); ok {
		t.Error("settings row must be removed")
	}
	got, _ := mgr.GetKey(follower.ID)
	if got.ResetScheduleSource != "default" || got.EffectiveResetSchedule != nil {
		t.Errorf("follower: source=%s eff=%+v", got.ResetScheduleSource, got.EffectiveResetSchedule)
	}
	if got.QuotaUsage != before.QuotaUsage {
		t.Errorf("usage changed on removal: %d -> %d", before.QuotaUsage, got.QuotaUsage)
	}
	if _, ok := got.ResetRule.Pins["monthly"]; !ok {
		t.Errorf("removal must pin followers (window start moved back): %+v", got.ResetRule.Pins)
	}
}

func TestSetGlobalScheduleSameValueDoesNotChurnPins(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("k", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err != nil {
		t.Fatal(err)
	}
	first := pinsOf(t, d, k.ID)
	if first == "" {
		t.Fatal("precondition: first set must pin")
	}
	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err != nil {
		t.Fatal(err)
	}
	if got := pinsOf(t, d, k.ID); got != first {
		t.Errorf("saving the same schedule must keep pins: %q -> %q", first, got)
	}
}

func TestSetGlobalScheduleValidates(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("k", "all", nil, nil)
	for _, bad := range []*traffic.ResetSchedule{
		{Timezone: "Not/AZone", Time: "00:00", Weekday: 1, Day: 1},
		{Timezone: "UTC", Time: "24:00", Weekday: 1, Day: 1},
		{Timezone: "UTC", Time: "00:00", Weekday: 0, Day: 1},
		{Timezone: "UTC", Time: "00:00", Weekday: 1, Day: 32},
	} {
		if _, err := mgr.SetGlobalSchedule(bad); err == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
	if _, ok := globalRow(t, d); ok || pinsOf(t, d, k.ID) != "" {
		t.Error("a rejected schedule must write nothing")
	}
}

// A failure while writing any key rolls back the settings row and every pin.
func TestSetGlobalScheduleRollsBack(t *testing.T) {
	mgr, d := newMgr(t)
	a, _ := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	boom, _ := mgr.CreateKeyWithOptions("boom", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	if _, err := d.Exec(`CREATE TRIGGER fail_pins BEFORE UPDATE OF quota_window_pins ON api_keys
		WHEN NEW.id = '` + boom.ID + `' BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err == nil {
		t.Fatal("expected the forced failure")
	}
	if _, ok := globalRow(t, d); ok {
		t.Error("settings row must roll back")
	}
	if pinsOf(t, d, a.ID) != "" || pinsOf(t, d, boom.ID) != "" {
		t.Errorf("pins must roll back: a=%q boom=%q", pinsOf(t, d, a.ID), pinsOf(t, d, boom.ID))
	}
	got, _ := mgr.GetKey(a.ID)
	if got.ResetScheduleSource != "default" {
		t.Errorf("in-memory global must stay unset after a failed write, source=%s", got.ResetScheduleSource)
	}
}

func TestClearScheduleOverrides(t *testing.T) {
	mgr, d := newMgr(t)
	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err != nil {
		t.Fatal(err)
	}
	custom, _ := mgr.CreateKeyWithOptions("custom", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly", ResetSchedule: jkt("09:00", 2, 5)})
	other, _ := mgr.CreateKeyWithOptions("other", keys.CreateKeyOptions{ResetSchedule: jkt("00:00", 1, 1)})
	follower, _ := mgr.CreateKey("follower", "all", nil, nil)
	// usage inside the custom key's own current window (its monthly reset is day 5, 09:00 Jakarta)
	customNow, _ := mgr.GetKey(custom.ID)
	customStart, _ := traffic.CalcQuotaWindow("monthly", customNow.ResetRule, time.Now())
	insertUsage(t, d, custom.ID, "", 4100, customStart.Add(time.Minute))
	before, _ := mgr.GetKey(custom.ID)
	if before.QuotaUsage != 4100 {
		t.Fatalf("precondition: usage = %d, want 4100", before.QuotaUsage)
	}
	followerPins := pinsOf(t, d, follower.ID)

	counts, err := mgr.ClearScheduleOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if counts.Custom != 0 {
		t.Errorf("counts after clear = %+v", counts)
	}
	for _, id := range []string{custom.ID, other.ID} {
		if ownOf(t, d, id) != "" {
			t.Errorf("key %s still has its own schedule", id)
		}
		got, _ := mgr.GetKey(id)
		if got.ResetScheduleSource != "global" || got.EffectiveResetSchedule.Time != "14:00" {
			t.Errorf("key %s: source=%s eff=%+v", id, got.ResetScheduleSource, got.EffectiveResetSchedule)
		}
	}
	after, _ := mgr.GetKey(custom.ID)
	if after.QuotaUsage != before.QuotaUsage || after.QuotaUsage != 4100 {
		t.Errorf("clearing overrides changed usage: %d -> %d", before.QuotaUsage, after.QuotaUsage)
	}
	if _, ok := after.ResetRule.Pins["monthly"]; !ok {
		t.Errorf("formerly custom key must be pinned: %+v", after.ResetRule.Pins)
	}
	if pinsOf(t, d, follower.ID) != followerPins {
		t.Error("a key that already followed the global schedule must not be touched")
	}
}

func TestClearScheduleOverridesRollsBack(t *testing.T) {
	mgr, d := newMgr(t)
	if _, err := mgr.SetGlobalSchedule(jkt("14:00", 1, 20)); err != nil {
		t.Fatal(err)
	}
	a, _ := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly", ResetSchedule: jkt("09:00", 2, 5)})
	boom, _ := mgr.CreateKeyWithOptions("boom", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly", ResetSchedule: jkt("09:00", 2, 5)})
	if _, err := d.Exec(`CREATE TRIGGER fail_pins BEFORE UPDATE OF quota_window_pins ON api_keys
		WHEN NEW.id = '` + boom.ID + `' BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ClearScheduleOverrides(); err == nil {
		t.Fatal("expected the forced failure")
	}
	if ownOf(t, d, a.ID) == "" || ownOf(t, d, boom.ID) == "" {
		t.Error("overrides must be restored by the rollback")
	}
}
