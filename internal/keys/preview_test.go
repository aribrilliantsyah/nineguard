package keys_test

import (
	"reflect"
	"testing"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func TestPreviewWithoutKeyIsPureSchedule(t *testing.T) {
	mgr, _ := newMgr(t)
	now := time.Now().UTC()
	s := jkt("14:00", 3, 20)
	p, err := mgr.PreviewReset(s, "", now)
	if err != nil {
		t.Fatal(err)
	}
	for period, got := range map[string]string{"daily": p.Daily, "weekly": p.Weekly, "monthly": p.Monthly} {
		_, want := traffic.CalcQuotaWindow(period, traffic.ResetRule{Schedule: s}, now)
		if got != want.UTC().Format(time.RFC3339) {
			t.Errorf("%s = %s, want %s", period, got, want.UTC().Format(time.RFC3339))
		}
	}
	if p.Transitional == nil || len(p.Transitional) != 0 {
		t.Errorf("no key: transitional must be an empty list, got %#v", p.Transitional)
	}
}

func TestPreviewNilWithoutKeyIsDefault(t *testing.T) {
	mgr, _ := newMgr(t)
	now := time.Now().UTC()
	p, err := mgr.PreviewReset(nil, "", now)
	if err != nil {
		t.Fatal(err)
	}
	_, want := traffic.CalcQuotaWindow("monthly", traffic.ResetRule{}, now)
	if p.Monthly != want.UTC().Format(time.RFC3339) {
		t.Errorf("monthly = %s, want %s", p.Monthly, want.UTC().Format(time.RFC3339))
	}
}

// With a key, the preview equals what saving that schedule produces.
func TestPreviewWithKeyMatchesSave(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("k", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	s := jkt("00:00", 1, 20)

	p, err := mgr.PreviewReset(s, k.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Transitional, []string{"daily", "monthly"}) && !contains(p.Transitional, "monthly") {
		t.Errorf("monthly must be transitional: %v", p.Transitional)
	}

	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "k", ModelAccessMode: "all", ResetSchedule: &keys.ScheduleChange{Value: s}}); err != nil {
		t.Fatal(err)
	}
	saved, _ := mgr.GetKey(k.ID)
	now := time.Now()
	for period, got := range map[string]string{"daily": p.Daily, "weekly": p.Weekly, "monthly": p.Monthly} {
		_, want := traffic.CalcQuotaWindow(period, saved.ResetRule, now)
		if got != want.UTC().Format(time.RFC3339) {
			t.Errorf("%s: preview %s, saved %s", period, got, want.UTC().Format(time.RFC3339))
		}
	}
}

func TestPreviewWithKeyNilFollowsGlobal(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("k", keys.CreateKeyOptions{ResetSchedule: jkt("09:00", 2, 5)})
	g := jkt("14:00", 1, 20)
	if _, err := mgr.SetGlobalSchedule(g); err != nil {
		t.Fatal(err)
	}
	p, err := mgr.PreviewReset(nil, k.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !contains(p.Transitional, "monthly") {
		t.Errorf("going from a custom schedule to the global one moves the monthly window: %v", p.Transitional)
	}
}

func TestPreviewUnchangedScheduleHasNoTransitional(t *testing.T) {
	mgr, _ := newMgr(t)
	s := jkt("09:00", 2, 5)
	k, _ := mgr.CreateKeyWithOptions("k", keys.CreateKeyOptions{ResetSchedule: s})
	p, err := mgr.PreviewReset(s, k.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Transitional) != 0 {
		t.Errorf("re-saving the same schedule changes nothing: %v", p.Transitional)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("k", keys.CreateKeyOptions{QuotaLimit: 5000, QuotaPeriod: "monthly"})
	dump := func() string {
		var a, b, c string
		_ = d.QueryRow(`SELECT COALESCE(quota_reset_schedule,'-'), COALESCE(quota_window_pins,'-'), updated_at FROM api_keys WHERE id = ?`, k.ID).Scan(&a, &b, &c)
		var n int
		_ = d.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'quota_reset_schedule'`).Scan(&n)
		return a + "|" + b + "|" + c + "|" + string(rune('0'+n))
	}
	before := dump()
	if _, err := mgr.PreviewReset(jkt("00:00", 1, 20), k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.PreviewReset(jkt("00:00", 1, 20), "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := dump(); after != before {
		t.Errorf("preview wrote to the database:\n%s\n%s", before, after)
	}
}

func TestPreviewErrors(t *testing.T) {
	mgr, _ := newMgr(t)
	if _, err := mgr.PreviewReset(jkt("00:00", 1, 20), "missing", time.Now()); err == nil {
		t.Error("unknown key must fail")
	}
	if _, err := mgr.PreviewReset(&traffic.ResetSchedule{Timezone: "Not/AZone", Time: "00:00", Weekday: 1, Day: 1}, "", time.Now()); err == nil {
		t.Error("invalid schedule must fail")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
