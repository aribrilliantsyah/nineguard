package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func scheduleProxy(t *testing.T) (*db.DB, *keys.Manager, *traffic.Manager, *proxy.Proxy) {
	t.Helper()
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	p, err := proxy.NewProxy(models.NewManager(d), trafficMgr, keyMgr, providers.NewManager(d), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	return d, keyMgr, trafficMgr, p
}

func askChat(p *proxy.Proxy, rawKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	return w
}

// Tokens used before the scheduled window start no longer count; the 429 follows the schedule.
func TestKeyQuotaWindowFollowsSchedule(t *testing.T) {
	d, keyMgr, trafficMgr, p := scheduleProxy(t)
	_ = trafficMgr
	// daily reset 14:00 Jakarta = 07:00 UTC
	k, _ := keyMgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "daily",
		ResetSchedule: &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: "14:00", Weekday: 1, Day: 1}})

	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.UTC)
	if start.After(now) {
		start = start.AddDate(0, 0, -1)
	}
	insert := func(tokens int, at time.Time) {
		if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, ?, ?)`,
			k.ID, tokens, at.Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}
	insert(5000, start.Add(-time.Hour)) // previous window: does not count
	if w := askChat(p, k.RawKey); w.Code == http.StatusTooManyRequests {
		t.Fatalf("old window tokens must not block, got 429: %s", w.Body.String())
	}

	insert(1500, start.Add(time.Second)) // current window: over the limit
	w := askChat(p, k.RawKey)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	retry, _ := strconv.Atoi(w.Header().Get("Retry-After"))
	want := int(time.Until(start.AddDate(0, 0, 1)).Seconds())
	if retry < want-5 || retry > want+5 {
		t.Errorf("Retry-After = %d, want about %d (seconds to the 07:00 UTC reset)", retry, want)
	}
}

// The 429 over HTTP names the key's own timezone, for a Key Quota and a Model Quota alike.
func TestQuotaResponseNamesKeyTimezone(t *testing.T) {
	d, keyMgr, _, p := scheduleProxy(t)
	k, _ := keyMgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "daily",
		ResetSchedule: &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: "14:00", Weekday: 1, Day: 1}})
	def, _ := keyMgr.CreateKeyWithOptions("b", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "daily"})
	for _, id := range []string{k.ID, def.ID} {
		if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 5000, datetime('now'))`, id); err != nil {
			t.Fatal(err)
		}
	}

	w := askChat(p, k.RawKey)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "(at 14:00 WIB).") || strings.Contains(w.Body.String(), "UTC") {
		t.Errorf("scheduled key: %d\n%s", w.Code, w.Body.String())
	}
	w = askChat(p, def.RawKey)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "(at 00:00 UTC).") || strings.Contains(w.Body.String(), "WIB") {
		t.Errorf("default key: %d\n%s", w.Code, w.Body.String())
	}
}

// During a Transitional Window the reset, and Retry-After, are the pin's Until.
func TestRetryAfterUsesPinUntil(t *testing.T) {
	d, keyMgr, _, p := scheduleProxy(t)
	k, _ := keyMgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "monthly"})
	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 5000, ?)`,
		k.ID, firstOfMonth.Add(time.Minute).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if _, err := keyMgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all",
		ResetSchedule: &keys.ScheduleChange{Value: &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: "00:00", Weekday: 1, Day: 28}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := keyMgr.GetKey(k.ID)
	pin, ok := got.ResetRule.Pins["monthly"]
	if !ok {
		t.Skip("no monthly pin for this date (window start unchanged)")
	}
	w := askChat(p, k.RawKey)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
	retry, _ := strconv.Atoi(w.Header().Get("Retry-After"))
	want := int(time.Until(pin.Until).Seconds())
	if retry < want-5 || retry > want+5 {
		t.Errorf("Retry-After = %d, want about %d (seconds to the pin's Until)", retry, want)
	}
}

// The keep guarantee end to end: a blocked key stays blocked after its schedule is edited.
func TestBlockedKeyStaysBlockedAfterScheduleEdit(t *testing.T) {
	d, keyMgr, _, p := scheduleProxy(t)
	k, _ := keyMgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "monthly"})
	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 1500, ?)`,
		k.ID, firstOfMonth.Add(time.Minute).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if w := askChat(p, k.RawKey); w.Code != http.StatusTooManyRequests {
		t.Fatalf("precondition: want 429, got %d", w.Code)
	}

	// move the monthly reset to a different day in another timezone
	if _, err := keyMgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all",
		ResetSchedule: &keys.ScheduleChange{Value: &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: "00:00", Weekday: 1, Day: 20}}}); err != nil {
		t.Fatal(err)
	}
	if w := askChat(p, k.RawKey); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a schedule edit must not unblock the key, got %d: %s", w.Code, w.Body.String())
	}
}

// The reverse: a key under its limit is not pushed over it by an edit.
func TestOpenKeyStaysOpenAfterScheduleEdit(t *testing.T) {
	d, keyMgr, _, p := scheduleProxy(t)
	k, _ := keyMgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "monthly"})
	now := time.Now().UTC()
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	// 400 this window, 5000 in the last month: a recount from an earlier start would block
	if _, err := d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp) VALUES (?, 'm', 200, 400, ?), (?, 'm', 200, 5000, ?)`,
		k.ID, firstOfMonth.Add(time.Minute).Format("2006-01-02 15:04:05"),
		k.ID, firstOfMonth.AddDate(0, 0, -3).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	if _, err := keyMgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "a", ModelAccessMode: "all",
		ResetSchedule: &keys.ScheduleChange{Value: &traffic.ResetSchedule{Timezone: "Asia/Jakarta", Time: "00:00", Weekday: 1, Day: 28}}}); err != nil {
		t.Fatal(err)
	}
	if w := askChat(p, k.RawKey); w.Code == http.StatusTooManyRequests {
		t.Fatalf("a schedule edit must not block an open key: %s", w.Body.String())
	}
}
