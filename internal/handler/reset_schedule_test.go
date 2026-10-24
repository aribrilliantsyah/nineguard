package handler_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

const goodSched = `{"timezone":"Asia/Jakarta","time":"14:00","weekday":3,"day":20}`

func createKey(t *testing.T, e *mqH, body string) keys.KeyInfo {
	t.Helper()
	rec := e.call(e.h.CreateKey, "POST", "/api/v1/keys", body, "")
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var ki keys.KeyInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &ki); err != nil {
		t.Fatal(err)
	}
	return ki
}

func TestCreateKeyWithScheduleReturnsIt(t *testing.T) {
	e := newMQH(t)
	ki := createKey(t, e, `{"name":"a","quota_limit":1000,"quota_period":"daily","quota_reset_schedule":`+goodSched+`}`)
	if ki.ResetSchedule == nil || ki.ResetSchedule.Time != "14:00" || ki.ResetScheduleSource != "key" {
		t.Errorf("response: %+v source=%s", ki.ResetSchedule, ki.ResetScheduleSource)
	}
	if ki.EffectiveResetSchedule == nil || ki.EffectiveResetSchedule.Timezone != "Asia/Jakarta" {
		t.Errorf("effective: %+v", ki.EffectiveResetSchedule)
	}

	plain := createKey(t, e, `{"name":"b"}`)
	if plain.ResetSchedule != nil || plain.ResetScheduleSource != "default" {
		t.Errorf("no schedule sent: %+v source=%s", plain.ResetSchedule, plain.ResetScheduleSource)
	}
	// explicit null on create means "follow global" too
	nul := createKey(t, e, `{"name":"c","quota_reset_schedule":null}`)
	if nul.ResetSchedule != nil {
		t.Errorf("null on create: %+v", nul.ResetSchedule)
	}
}

func TestScheduleValidationReturns400NamingTheField(t *testing.T) {
	e := newMQH(t)
	cases := []struct{ name, sched, field string }{
		{"Local", `{"timezone":"Local","time":"14:00","weekday":1,"day":1}`, "timezone"},
		{"unknown", `{"timezone":"Not/AZone","time":"14:00","weekday":1,"day":1}`, "timezone"},
		{"empty", `{"timezone":"","time":"14:00","weekday":1,"day":1}`, "timezone"},
		{"too long", `{"timezone":"` + strings.Repeat("a", 65) + `","time":"14:00","weekday":1,"day":1}`, "timezone"},
		{"hour 24", `{"timezone":"UTC","time":"24:00","weekday":1,"day":1}`, "time"},
		{"short clock", `{"timezone":"UTC","time":"9:5","weekday":1,"day":1}`, "time"},
		{"weekday 0", `{"timezone":"UTC","time":"14:00","weekday":0,"day":1}`, "weekday"},
		{"weekday 8", `{"timezone":"UTC","time":"14:00","weekday":8,"day":1}`, "weekday"},
		{"day 0", `{"timezone":"UTC","time":"14:00","weekday":1,"day":0}`, "day"},
		{"day 32", `{"timezone":"UTC","time":"14:00","weekday":1,"day":32}`, "day"},
		{"missing timezone", `{"time":"14:00","weekday":1,"day":1}`, "timezone"},
		{"missing time", `{"timezone":"UTC","weekday":1,"day":1}`, "time"},
		{"missing weekday", `{"timezone":"UTC","time":"14:00","day":1}`, "weekday"},
		{"missing day", `{"timezone":"UTC","time":"14:00","weekday":1}`, "day"},
		{"not an object", `"Asia/Jakarta"`, "quota_reset_schedule"},
	}
	k := createKey(t, e, `{"name":"base"}`)
	for _, c := range cases {
		rec := e.call(e.h.CreateKey, "POST", "/api/v1/keys", `{"name":"x","quota_reset_schedule":`+c.sched+`}`, "")
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.field) {
			t.Errorf("create %s: %d %s (want 400 naming %q)", c.name, rec.Code, rec.Body.String(), c.field)
		}
		rec = e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"base","quota_reset_schedule":`+c.sched+`}`, k.ID)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.field) {
			t.Errorf("update %s: %d %s (want 400 naming %q)", c.name, rec.Code, rec.Body.String(), c.field)
		}
	}
	got, _ := e.km.GetKey(k.ID)
	if got.ResetSchedule != nil {
		t.Errorf("a rejected update must not store anything: %+v", got.ResetSchedule)
	}
	list, _ := e.km.ListKeys()
	if len(list) != 2 { // default key + base: nothing was created by the rejected creates
		t.Errorf("rejected creates must not create keys, have %d", len(list))
	}
}

func TestUpdateKeyScheduleAbsentNullObject(t *testing.T) {
	e := newMQH(t)
	k := createKey(t, e, `{"name":"a","quota_reset_schedule":`+goodSched+`}`)

	// absent: unchanged
	rec := e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"renamed"}`, k.ID)
	var ki keys.KeyInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &ki)
	if rec.Code != 200 || ki.Name != "renamed" || ki.ResetSchedule == nil || ki.ResetSchedule.Day != 20 {
		t.Fatalf("absent: %d %+v", rec.Code, ki.ResetSchedule)
	}

	// object: replaces
	rec = e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"renamed","quota_reset_schedule":{"timezone":"UTC","time":"06:30","weekday":1,"day":1}}`, k.ID)
	ki = keys.KeyInfo{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ki)
	if rec.Code != 200 || ki.ResetSchedule == nil || ki.ResetSchedule.Time != "06:30" || ki.ResetSchedule.Timezone != "UTC" {
		t.Fatalf("object: %d %+v", rec.Code, ki.ResetSchedule)
	}

	// null: clears (follows global)
	rec = e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"renamed","quota_reset_schedule":null}`, k.ID)
	ki = keys.KeyInfo{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ki)
	if rec.Code != 200 || ki.ResetSchedule != nil || ki.ResetScheduleSource != "default" {
		t.Fatalf("null: %d %+v source=%s", rec.Code, ki.ResetSchedule, ki.ResetScheduleSource)
	}
}

func TestScheduleAcceptedForUnusedFieldsAndPeriods(t *testing.T) {
	e := newMQH(t)
	// daily period: weekday and day are unused but kept (ADR 0010 #4); none/total accept a schedule too
	for _, period := range []string{"daily", "none", "total"} {
		ki := createKey(t, e, `{"name":"`+period+`","quota_limit":10,"quota_period":"`+period+`","quota_reset_schedule":`+goodSched+`}`)
		if ki.ResetSchedule == nil || ki.ResetSchedule.Weekday != 3 || ki.ResetSchedule.Day != 20 {
			t.Errorf("%s: unused fields dropped: %+v", period, ki.ResetSchedule)
		}
	}
}

func TestKeyReadsCarryTheScheduleFields(t *testing.T) {
	e := newMQH(t)
	k := createKey(t, e, `{"name":"a","quota_reset_schedule":`+goodSched+`}`)

	rec := e.call(e.h.GetKey, "GET", "/api/v1/keys/"+k.ID, "", k.ID)
	var one map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	for _, f := range []string{"quota_reset_schedule", "effective_quota_reset_schedule", "quota_reset_schedule_source"} {
		if _, ok := one[f]; !ok {
			t.Errorf("GET key missing %s", f)
		}
	}

	for _, url := range []string{"/api/v1/keys", "/api/v1/keys?page=1&limit=50"} {
		rec = e.call(e.h.ListKeys, "GET", url, "", "")
		var list struct {
			Keys []map[string]any `json:"keys"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &list)
		found := false
		for _, kk := range list.Keys {
			if kk["id"] == k.ID {
				found = true
				if kk["quota_reset_schedule_source"] != "key" || kk["quota_reset_schedule"] == nil || kk["effective_quota_reset_schedule"] == nil {
					t.Errorf("%s: %v", url, kk)
				}
			}
		}
		if !found {
			t.Errorf("%s: key missing", url)
		}
	}
}

func TestQuotasOverviewCarriesScheduleAndPinnedReset(t *testing.T) {
	e := newMQH(t)
	k := createKey(t, e, `{"name":"a","quota_limit":5000,"quota_period":"monthly"}`)
	now := time.Now().UTC()
	if _, err := e.d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 100, ?)`,
		k.ID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Add(time.Minute).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if rec := e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"a","quota_reset_schedule":{"timezone":"Asia/Jakarta","time":"00:00","weekday":1,"day":28}}`, k.ID); rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	saved, _ := e.km.GetKey(k.ID)
	pin, pinned := saved.ResetRule.Pins["monthly"]

	rec := e.call(e.h.GetQuotas, "GET", "/api/v1/quotas", "", "")
	var ov struct {
		Keys []struct {
			ID       string `json:"id"`
			KeyQuota *struct {
				ResetAt  *string                `json:"reset_at"`
				Schedule *traffic.ResetSchedule `json:"quota_reset_schedule"`
				Source   string                 `json:"quota_reset_schedule_source"`
				Usage    int64                  `json:"usage"`
			} `json:"key_quota"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	for _, ok := range ov.Keys {
		if ok.ID != k.ID {
			continue
		}
		if ok.KeyQuota == nil || ok.KeyQuota.Schedule == nil || ok.KeyQuota.Schedule.Day != 28 || ok.KeyQuota.Source != "key" {
			t.Fatalf("key_quota schedule missing: %+v", ok.KeyQuota)
		}
		if ok.KeyQuota.Usage != 100 {
			t.Errorf("usage = %d, want 100", ok.KeyQuota.Usage)
		}
		if pinned && (ok.KeyQuota.ResetAt == nil || *ok.KeyQuota.ResetAt != pin.Until.UTC().Format(time.RFC3339)) {
			t.Errorf("reset_at = %v, want the pin's Until %s", ok.KeyQuota.ResetAt, pin.Until.UTC().Format(time.RFC3339))
		}
		return
	}
	t.Fatal("key missing from overview")
}

func TestGlobalScheduleEndpoints(t *testing.T) {
	e := newMQH(t)
	createKey(t, e, `{"name":"follower"}`)
	custom := createKey(t, e, `{"name":"custom","quota_reset_schedule":`+goodSched+`}`)

	type resp struct {
		Schedule  *traffic.ResetSchedule `json:"quota_reset_schedule"`
		Following int                    `json:"following_keys"`
		Custom    int                    `json:"custom_keys"`
	}
	decode := func(b []byte) resp {
		var r resp
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	rec := e.call(e.h.GetGlobalResetSchedule, "GET", "/api/v1/settings/quota-reset-schedule", "", "")
	r := decode(rec.Body.Bytes())
	if rec.Code != 200 || r.Schedule != nil || r.Custom != 1 || r.Following < 1 {
		t.Fatalf("initial: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.call(e.h.SetGlobalResetSchedule, "POST", "/api/v1/settings/quota-reset-schedule", `{"quota_reset_schedule":{"timezone":"Asia/Jakarta","time":"14:00","weekday":1,"day":1}}`, "")
	r = decode(rec.Body.Bytes())
	if rec.Code != 200 || r.Schedule == nil || r.Schedule.Time != "14:00" || r.Custom != 1 {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	if g := e.km.GetGlobalSchedule(); g == nil || g.Timezone != "Asia/Jakarta" {
		t.Errorf("manager global = %+v", g)
	}

	// clear overrides
	rec = e.call(e.h.ClearResetScheduleOverrides, "POST", "/api/v1/settings/quota-reset-schedule/clear-overrides", "", "")
	r = decode(rec.Body.Bytes())
	if rec.Code != 200 || r.Custom != 0 || r.Schedule == nil {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := e.km.GetKey(custom.ID)
	if got.ResetSchedule != nil || got.ResetScheduleSource != "global" {
		t.Errorf("custom key after clear: %+v source=%s", got.ResetSchedule, got.ResetScheduleSource)
	}

	// null removes it
	rec = e.call(e.h.SetGlobalResetSchedule, "POST", "/api/v1/settings/quota-reset-schedule", `{"quota_reset_schedule":null}`, "")
	r = decode(rec.Body.Bytes())
	if rec.Code != 200 || r.Schedule != nil {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGlobalScheduleValidation(t *testing.T) {
	e := newMQH(t)
	for _, c := range []struct{ body, field string }{
		{`{"quota_reset_schedule":{"timezone":"Not/AZone","time":"14:00","weekday":1,"day":1}}`, "timezone"},
		{`{"quota_reset_schedule":{"timezone":"UTC","time":"25:00","weekday":1,"day":1}}`, "time"},
		{`{"quota_reset_schedule":{"timezone":"UTC","time":"14:00","weekday":9,"day":1}}`, "weekday"},
		{`{"quota_reset_schedule":{"timezone":"UTC","time":"14:00","weekday":1,"day":40}}`, "day"},
		{`{"quota_reset_schedule":{"timezone":"UTC","time":"14:00","weekday":1}}`, "day"},
		{`{}`, "quota_reset_schedule"},
		{`not json`, "payload"},
	} {
		rec := e.call(e.h.SetGlobalResetSchedule, "POST", "/api/v1/settings/quota-reset-schedule", c.body, "")
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.field) {
			t.Errorf("%s: %d %s (want 400 naming %q)", c.body, rec.Code, rec.Body.String(), c.field)
		}
	}
	if g := e.km.GetGlobalSchedule(); g != nil {
		t.Errorf("a rejected body must set nothing: %+v", g)
	}
}

func TestResetPreviewEndpoint(t *testing.T) {
	e := newMQH(t)
	k := createKey(t, e, `{"name":"a","quota_limit":5000,"quota_period":"monthly"}`)

	type prev struct {
		Daily, Weekly, Monthly string
		Transitional           []string `json:"transitional"`
	}
	run := func(body string) (prev, int) {
		rec := e.call(e.h.PreviewQuotaReset, "POST", "/api/v1/quotas/reset-preview", body, "")
		var p prev
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		p.Daily, _ = raw["daily"].(string)
		p.Weekly, _ = raw["weekly"].(string)
		p.Monthly, _ = raw["monthly"].(string)
		return p, rec.Code
	}

	// without key: pure schedule, no pins
	p, code := run(`{"quota_reset_schedule":` + goodSched + `}`)
	if code != 200 || p.Daily == "" || p.Weekly == "" || p.Monthly == "" || p.Transitional == nil || len(p.Transitional) != 0 {
		t.Fatalf("no key: %d %+v", code, p)
	}

	// with key: matches what a subsequent save produces
	p, code = run(`{"key_id":"` + k.ID + `","quota_reset_schedule":{"timezone":"Asia/Jakarta","time":"00:00","weekday":1,"day":28}}`)
	if code != 200 {
		t.Fatalf("with key: %d", code)
	}
	before, _ := e.km.GetKey(k.ID)
	if before.ResetSchedule != nil {
		t.Fatal("preview must not save anything")
	}
	if rec := e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, `{"name":"a","quota_reset_schedule":{"timezone":"Asia/Jakarta","time":"00:00","weekday":1,"day":28}}`, k.ID); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	saved, _ := e.km.GetKey(k.ID)
	_, want := traffic.CalcQuotaWindow("monthly", saved.ResetRule, time.Now())
	if p.Monthly != want.UTC().Format(time.RFC3339) {
		t.Errorf("preview monthly %s != saved %s", p.Monthly, want.UTC().Format(time.RFC3339))
	}

	// errors
	for _, c := range []struct {
		body string
		code int
	}{
		{`{"quota_reset_schedule":{"timezone":"Not/AZone","time":"14:00","weekday":1,"day":1}}`, 400},
		{`{"key_id":"missing","quota_reset_schedule":` + goodSched + `}`, 404},
		{`not json`, 400},
	} {
		if _, code := run(c.body); code != c.code {
			t.Errorf("%s: %d, want %d", c.body, code, c.code)
		}
	}
}
