package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func jkt(clock string, weekday, day int) *traffic.ResetSchedule {
	return &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: clock, Weekday: weekday, Day: day}
}

func newMgr(t *testing.T) (*keys.Manager, *db.DB) {
	t.Helper()
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return keys.NewManager(d, ""), d
}

func set(s *traffic.ResetSchedule) *keys.ScheduleChange { return &keys.ScheduleChange{Value: s} }

func TestNewKeyHasNoScheduleAndUsesDefault(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	got, err := mgr.GetKey(k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResetSchedule != nil || got.EffectiveResetSchedule != nil {
		t.Errorf("want nil schedules, got %v %v", got.ResetSchedule, got.EffectiveResetSchedule)
	}
	if got.ResetScheduleSource != "default" {
		t.Errorf("source = %q, want default", got.ResetScheduleSource)
	}
	if got.ResetRule.Schedule != nil || len(got.ResetRule.Pins) != 0 {
		t.Errorf("rule should be the zero rule, got %+v", got.ResetRule)
	}
}

func TestResolutionKeyBeatsGlobalBeatsDefault(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)

	// global only
	if _, err := d.Exec(`INSERT INTO settings (key, value) VALUES ('quota_reset_schedule', ?)`,
		`{"timezone":"Asia/Jakarta","time":"14:00","weekday":1,"day":1}`); err != nil {
		t.Fatal(err)
	}
	mgr.ReloadGlobalSchedule()
	got, _ := mgr.GetKey(k.ID)
	if got.ResetScheduleSource != "global" || got.EffectiveResetSchedule == nil || got.EffectiveResetSchedule.Time != "14:00" || got.ResetSchedule != nil {
		t.Errorf("global: %+v source=%s", got.EffectiveResetSchedule, got.ResetScheduleSource)
	}

	// key schedule wins
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", ResetSchedule: set(jkt("09:30", 2, 5))}); err != nil {
		t.Fatal(err)
	}
	got, _ = mgr.GetKey(k.ID)
	if got.ResetScheduleSource != "key" || got.EffectiveResetSchedule.Time != "09:30" || got.ResetSchedule == nil || got.ResetSchedule.Day != 5 {
		t.Errorf("key: %+v source=%s", got.EffectiveResetSchedule, got.ResetScheduleSource)
	}

	// clearing the key schedule falls back to global
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", ResetSchedule: &keys.ScheduleChange{Clear: true}}); err != nil {
		t.Fatal(err)
	}
	got, _ = mgr.GetKey(k.ID)
	if got.ResetScheduleSource != "global" || got.ResetSchedule != nil {
		t.Errorf("after clear: source=%s own=%v", got.ResetScheduleSource, got.ResetSchedule)
	}
}

func TestUpdateKeyAllScheduleAbsentKeepsPresentSets(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", ResetSchedule: set(jkt("14:00", 3, 20))}); err != nil {
		t.Fatal(err)
	}
	// absent: untouched
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "renamed", ModelAccessMode: "all"}); err != nil {
		t.Fatal(err)
	}
	got, _ := mgr.GetKey(k.ID)
	if got.Name != "renamed" || got.ResetSchedule == nil || *got.ResetSchedule != *jkt("14:00", 3, 20) {
		t.Fatalf("schedule must survive an update without it: %+v", got.ResetSchedule)
	}
	var raw string
	if err := d.QueryRow(`SELECT quota_reset_schedule FROM api_keys WHERE id = ?`, k.ID).Scan(&raw); err != nil || raw == "" {
		t.Errorf("stored JSON missing: %q %v", raw, err)
	}
}

// Editing a schedule keeps the running window: pins are written in the same transaction.
func TestUpdateKeyAllWritesPinsAndKeepsUsage(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	period := "monthly"
	limit := int64(5000)
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 4100, ?)`,
		k.ID, startOfMonth.Add(time.Minute).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", QuotaLimit: &limit, QuotaPeriod: &period}); err != nil {
		t.Fatal(err)
	}
	before, _ := mgr.GetKey(k.ID)

	// move the monthly reset to a day that is not the 1st, in another timezone
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", ResetSchedule: set(jkt("00:00", 1, 20))}); err != nil {
		t.Fatal(err)
	}
	after, _ := mgr.GetKey(k.ID)

	if before.QuotaUsage != 4100 || after.QuotaUsage != before.QuotaUsage {
		t.Errorf("usage must not change on a schedule edit: before %d after %d", before.QuotaUsage, after.QuotaUsage)
	}
	if _, ok := after.ResetRule.Pins["monthly"]; !ok {
		t.Errorf("monthly pin missing: %+v", after.ResetRule.Pins)
	}
	var rawPins string
	if err := d.QueryRow(`SELECT COALESCE(quota_window_pins,'') FROM api_keys WHERE id = ?`, k.ID).Scan(&rawPins); err != nil || rawPins == "" {
		t.Errorf("pins must be persisted: %q %v", rawPins, err)
	}
}

func TestUpdateKeyAllScheduleRollsBackWithKeyFields(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("orig", "all", nil, nil)
	dup := []keys.ModelQuota{
		{ModelID: "p/a", QuotaLimit: 1, QuotaPeriod: "daily"},
		{ModelID: "P/A", QuotaLimit: 2, QuotaPeriod: "daily"},
	}
	_, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "changed", ModelAccessMode: "all", ResetSchedule: set(jkt("14:00", 1, 20)), ModelQuotas: &dup})
	if err == nil {
		t.Fatal("duplicate rows must fail")
	}
	got, _ := mgr.GetKey(k.ID)
	if got.Name != "orig" || got.ResetSchedule != nil {
		t.Errorf("schedule and name must roll back together: name=%q sched=%v", got.Name, got.ResetSchedule)
	}
	var pins *string
	_ = d.QueryRow(`SELECT quota_window_pins FROM api_keys WHERE id = ?`, k.ID).Scan(&pins)
	if pins != nil {
		t.Errorf("pins must roll back, got %q", *pins)
	}
}

func TestCreateKeyWithScheduleStoresItWithoutPins(t *testing.T) {
	mgr, _ := newMgr(t)
	ki, err := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{ResetSchedule: jkt("14:00", 3, 20)})
	if err != nil {
		t.Fatal(err)
	}
	if ki.ResetSchedule == nil || ki.ResetScheduleSource != "key" || len(ki.ResetRule.Pins) != 0 {
		t.Errorf("created key: %+v source=%s pins=%v", ki.ResetSchedule, ki.ResetScheduleSource, ki.ResetRule.Pins)
	}
	got, _ := mgr.GetKey(ki.ID)
	if got.ResetSchedule == nil || got.ResetSchedule.Day != 20 {
		t.Errorf("not persisted: %+v", got.ResetSchedule)
	}
}

// Every load path must return the schedule fields.
func TestScheduleVisibleOnAllLoadPaths(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{ResetSchedule: jkt("14:00", 3, 20)})

	check := func(path string, ki *keys.KeyInfo) {
		t.Helper()
		if ki == nil || ki.ResetSchedule == nil || ki.ResetSchedule.Time != "14:00" || ki.ResetScheduleSource != "key" ||
			ki.ResetRule.Schedule == nil || ki.EffectiveResetSchedule == nil {
			t.Errorf("%s: schedule missing: %+v", path, ki)
		}
	}
	got, _ := mgr.GetKey(k.ID)
	check("GetKey", got)

	list, err := mgr.ListKeys()
	if err != nil || len(list) == 0 {
		t.Fatalf("ListKeys: %v", err)
	}
	for i := range list {
		if list[i].ID == k.ID {
			check("ListKeys", &list[i])
		}
	}
	page, err := mgr.ListKeysPage(keys.ListOptions{Page: 1, Limit: 50})
	if err != nil {
		t.Fatalf("ListKeysPage: %v", err)
	}
	for i := range page.Keys {
		if page.Keys[i].ID == k.ID {
			check("ListKeysPage", &page.Keys[i])
		}
	}

	// cached path
	vk, ok := mgr.ValidateClientKey(k.RawKey)
	if !ok {
		t.Fatal("key must validate")
	}
	check("ValidateClientKey (cache)", vk)

	// SQLite fallback path: a second manager has a cold cache for keys added behind its back
	other := keys.NewManager(d, "")
	if _, err := d.Exec(`INSERT INTO api_keys (id, key, prefix, name, is_active, quota_reset_schedule) VALUES ('late', 'sk-ng-late', 'sk-ng-', 'late', 1, ?)`,
		`{"timezone":"Asia/Jakarta","time":"14:00","weekday":3,"day":20}`); err != nil {
		t.Fatal(err)
	}
	fb, ok := other.ValidateClientKey("sk-ng-late")
	if !ok {
		t.Fatal("fallback key must validate")
	}
	check("ValidateClientKey (fallback)", fb)
}

func TestModeAndPeriodSwitchKeepScheduleAndPins(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	period := "monthly"
	mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", QuotaPeriod: &period, ResetSchedule: set(jkt("00:00", 1, 20))})
	before, _ := mgr.GetKey(k.ID)

	mode := "model"
	daily := "daily"
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all", QuotaMode: &mode, QuotaPeriod: &daily}); err != nil {
		t.Fatal(err)
	}
	after, _ := mgr.GetKey(k.ID)
	if after.ResetSchedule == nil || *after.ResetSchedule != *before.ResetSchedule {
		t.Errorf("schedule changed by mode/period switch: %v -> %v", before.ResetSchedule, after.ResetSchedule)
	}
	if len(after.ResetRule.Pins) != len(before.ResetRule.Pins) {
		t.Errorf("pins changed by mode/period switch: %v -> %v", before.ResetRule.Pins, after.ResetRule.Pins)
	}
}

func TestBadStoredJSONIsIgnored(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	if _, err := d.Exec(`UPDATE api_keys SET quota_reset_schedule = 'not json', quota_window_pins = '{"monthly": 5}' WHERE id = ?`, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('quota_reset_schedule', '{broken')`); err != nil {
		t.Fatal(err)
	}
	mgr.ReloadGlobalSchedule()
	got, err := mgr.GetKey(k.ID)
	if err != nil {
		t.Fatalf("key must still load: %v", err)
	}
	if got.ResetSchedule != nil || got.EffectiveResetSchedule != nil || got.ResetScheduleSource != "default" || len(got.ResetRule.Pins) != 0 {
		t.Errorf("bad JSON must act as absent: %+v source=%s", got.ResetSchedule, got.ResetScheduleSource)
	}
}

func TestInvalidStoredScheduleIsIgnored(t *testing.T) {
	mgr, d := newMgr(t)
	k, _ := mgr.CreateKey("a", "all", nil, nil)
	// well-formed JSON but a timezone that does not exist
	if _, err := d.Exec(`UPDATE api_keys SET quota_reset_schedule = ? WHERE id = ?`,
		`{"timezone":"Not/AZone","time":"14:00","weekday":1,"day":1}`, k.ID); err != nil {
		t.Fatal(err)
	}
	mgr.ReloadGlobalSchedule()
	got, _ := mgr.GetKey(k.ID)
	if got.ResetRule.Schedule != nil {
		t.Errorf("an invalid stored schedule must not reach window math, got %+v", got.ResetRule.Schedule)
	}
}
