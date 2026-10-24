package proxy

import (
	"strings"
	"testing"
	"time"

	"nineguard/internal/traffic"
)

func ruleFor(tz, clock string, weekday, day int) traffic.ResetRule {
	return traffic.ResetRule{Schedule: &traffic.ResetSchedule{Timezone: tz, Time: clock, Weekday: weekday, Day: day}}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestQuotaMessageShowsScheduleTimezone(t *testing.T) {
	cases := []struct {
		name, period string
		rule         traffic.ResetRule
		now, reset   string
		want         []string
		notWant      []string
	}{
		{"daily Jakarta", "daily", ruleFor("Asia/Jakarta", "14:00", 1, 1),
			"2026-10-17T04:00:00Z", "2026-10-17T07:00:00Z",
			[]string{"Resets in 3h 0m (at 14:00 WIB)."}, []string{"UTC", "WIB /"}},
		{"weekly Jakarta", "weekly", ruleFor("Asia/Jakarta", "14:00", 1, 1),
			"2026-10-30T01:00:00Z", "2026-11-02T07:00:00Z",
			[]string{"Resets in 3d 6h (at 14:00 WIB on 02 Nov)."}, []string{"UTC"}},
		{"monthly Jakarta", "monthly", ruleFor("Asia/Jakarta", "14:00", 1, 1),
			"2026-10-30T01:00:00Z", "2026-11-02T07:00:00Z",
			[]string{"(at 14:00 WIB on 02 Nov)."}, nil},
		{"default is UTC", "daily", traffic.ResetRule{},
			"2026-10-17T21:00:00Z", "2026-10-18T00:00:00Z",
			[]string{"Resets in 3h 0m (at 00:00 UTC)."}, []string{"WIB"}},
		{"default monthly is UTC", "monthly", traffic.ResetRule{},
			"2026-10-30T00:00:00Z", "2026-11-01T00:00:00Z",
			[]string{"Resets in 2d 0h (at 00:00 UTC on 01 Nov)."}, nil},
		{"New York abbreviation", "daily", ruleFor("America/New_York", "09:00", 1, 1),
			"2026-10-17T10:00:00Z", "2026-10-17T13:00:00Z",
			[]string{"(at 09:00 EDT)."}, nil},
		// Go reports offsets such as "+04" for zones without a letter abbreviation: use the IANA name.
		{"numeric abbreviation falls back to name", "daily", ruleFor("Asia/Dubai", "14:00", 1, 1),
			"2026-10-17T04:00:00Z", "2026-10-17T10:00:00Z",
			[]string{"(at 14:00 Asia/Dubai)."}, []string{"+04"}},
	}
	for _, c := range cases {
		body, _ := quotaExceededBody("", 1500, 1000, c.period, c.rule, mustTime(c.reset), mustTime(c.now))
		s := string(body)
		for _, w := range c.want {
			if !strings.Contains(s, w) {
				t.Errorf("%s: missing %q in:\n%s", c.name, w, s)
			}
		}
		for _, n := range c.notWant {
			if strings.Contains(s, n) {
				t.Errorf("%s: must not contain %q in:\n%s", c.name, n, s)
			}
		}
	}
}

func TestQuotaMessageNoHardcodedZone(t *testing.T) {
	// Asia/Jakarta used to be hardcoded; a non-Jakarta zone must never show WIB.
	body, _ := quotaExceededBody("", 2, 1, "daily", ruleFor("Europe/Berlin", "02:30", 1, 1),
		mustTime("2026-10-17T12:00:00Z"), mustTime("2026-10-17T09:00:00Z"))
	s := string(body)
	if strings.Contains(s, "WIB") || !strings.Contains(s, "CEST") {
		t.Errorf("want CEST and no WIB:\n%s", s)
	}
}

func TestQuotaMessageDaysAndHours(t *testing.T) {
	now := mustTime("2026-10-17T00:00:00Z")
	cases := []struct {
		until time.Duration
		want  string
	}{
		{45 * time.Minute, "Resets in 45m ("},
		{3*time.Hour + 5*time.Minute, "Resets in 3h 5m ("},
		{23*time.Hour + 59*time.Minute, "Resets in 23h 59m ("},
		{24 * time.Hour, "Resets in 1d 0h ("},
		{53 * time.Hour, "Resets in 2d 5h ("},
		{7*24*time.Hour + 2*time.Hour, "Resets in 7d 2h ("},
	}
	for _, c := range cases {
		body, _ := quotaExceededBody("", 2, 1, "weekly", traffic.ResetRule{}, now.Add(c.until), now)
		if !strings.Contains(string(body), c.want) {
			t.Errorf("%v: want %q in:\n%s", c.until, c.want, body)
		}
	}
}

func TestRetryAfterIsSecondsToTheScheduledReset(t *testing.T) {
	now := mustTime("2026-10-17T04:00:00Z")
	_, ra := quotaExceededBody("", 2, 1, "daily", ruleFor("Asia/Jakarta", "14:00", 1, 1), mustTime("2026-10-17T07:00:00Z"), now)
	if ra != 3*3600 {
		t.Errorf("Retry-After = %d, want %d", ra, 3*3600)
	}
}

func TestTotalQuotaMessageIgnoresSchedule(t *testing.T) {
	body, ra := quotaExceededBody("p/a", 9, 5, "total", ruleFor("Asia/Jakarta", "14:00", 1, 1),
		time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), mustTime("2026-10-17T04:00:00Z"))
	s := string(body)
	if ra != 0 || !strings.Contains(s, "Lifetime quota, does not reset.") || strings.Contains(s, "WIB") || strings.Contains(s, "Resets in") {
		t.Errorf("total: ra=%d\n%s", ra, s)
	}
}

func TestZoneLabel(t *testing.T) {
	cases := []struct {
		tz   string
		at   string
		want string
	}{
		{"Asia/Jakarta", "2026-10-17T00:00:00Z", "WIB"},
		{"UTC", "2026-10-17T00:00:00Z", "UTC"},
		{"America/New_York", "2026-01-15T12:00:00Z", "EST"},
		{"America/New_York", "2026-07-15T12:00:00Z", "EDT"},
		{"Asia/Dubai", "2026-10-17T00:00:00Z", "Asia/Dubai"},
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.tz)
		if err != nil {
			t.Fatal(err)
		}
		if got := zoneLabel(mustTime(c.at).In(loc), c.tz); got != c.want {
			t.Errorf("%s at %s = %q, want %q", c.tz, c.at, got, c.want)
		}
	}
}
