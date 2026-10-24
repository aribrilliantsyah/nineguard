package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/traffic"
)

func rule(s *traffic.ResetSchedule) traffic.ResetRule { return traffic.ResetRule{Schedule: s} }

func TestCalcQuotaWindowWithSchedule(t *testing.T) {
	cases := []struct {
		name          string
		period        string
		s             *traffic.ResetSchedule
		now           string
		start, resets string
	}{
		// daily, Asia/Jakarta = UTC+7
		{"daily before reset time", "daily", sched("Asia/Jakarta", "14:00", 1, 1), "2026-10-17T03:00:00Z", "2026-10-16T07:00:00Z", "2026-10-17T07:00:00Z"},
		{"daily after reset time", "daily", sched("Asia/Jakarta", "14:00", 1, 1), "2026-10-17T08:00:00Z", "2026-10-17T07:00:00Z", "2026-10-18T07:00:00Z"},
		{"daily exactly at reset", "daily", sched("Asia/Jakarta", "14:00", 1, 1), "2026-10-17T07:00:00Z", "2026-10-17T07:00:00Z", "2026-10-18T07:00:00Z"},
		{"daily 23:59", "daily", sched("Asia/Jakarta", "23:59", 1, 1), "2026-10-17T03:00:00Z", "2026-10-16T16:59:00Z", "2026-10-17T16:59:00Z"},
		{"daily 00:00", "daily", sched("Asia/Jakarta", "00:00", 1, 1), "2026-10-17T03:00:00Z", "2026-10-16T17:00:00Z", "2026-10-17T17:00:00Z"},
		// weekly, 2026-10-07 is a Wednesday
		{"weekly Wed before", "weekly", sched("Asia/Jakarta", "14:00", 3, 1), "2026-10-07T03:00:00Z", "2026-09-30T07:00:00Z", "2026-10-07T07:00:00Z"},
		{"weekly Wed after", "weekly", sched("Asia/Jakarta", "14:00", 3, 1), "2026-10-07T08:00:00Z", "2026-10-07T07:00:00Z", "2026-10-14T07:00:00Z"},
		{"weekly Wed on Saturday", "weekly", sched("Asia/Jakarta", "14:00", 3, 1), "2026-10-10T00:00:00Z", "2026-10-07T07:00:00Z", "2026-10-14T07:00:00Z"},
		{"weekly Wed on Monday", "weekly", sched("Asia/Jakarta", "14:00", 3, 1), "2026-10-05T00:00:00Z", "2026-09-30T07:00:00Z", "2026-10-07T07:00:00Z"},
		{"weekly Sunday", "weekly", sched("Asia/Jakarta", "00:00", 7, 1), "2026-10-11T10:00:00Z", "2026-10-10T17:00:00Z", "2026-10-17T17:00:00Z"},
		{"weekly Monday", "weekly", sched("Asia/Jakarta", "00:00", 1, 1), "2026-10-07T03:00:00Z", "2026-10-04T17:00:00Z", "2026-10-11T17:00:00Z"},
		// monthly
		{"monthly day 15 after", "monthly", sched("Asia/Jakarta", "14:00", 1, 15), "2026-10-17T03:00:00Z", "2026-10-15T07:00:00Z", "2026-11-15T07:00:00Z"},
		{"monthly day 15 before", "monthly", sched("Asia/Jakarta", "14:00", 1, 15), "2026-10-10T03:00:00Z", "2026-09-15T07:00:00Z", "2026-10-15T07:00:00Z"},
		{"monthly day 1", "monthly", sched("Asia/Jakarta", "00:00", 1, 1), "2026-10-17T03:00:00Z", "2026-09-30T17:00:00Z", "2026-10-31T17:00:00Z"},
		{"monthly day 31 in Feb before", "monthly", sched("UTC", "00:00", 1, 31), "2026-02-15T00:00:00Z", "2026-01-31T00:00:00Z", "2026-02-28T00:00:00Z"},
		{"monthly day 31 in Mar 1", "monthly", sched("UTC", "00:00", 1, 31), "2026-03-01T00:00:00Z", "2026-02-28T00:00:00Z", "2026-03-31T00:00:00Z"},
		{"monthly day 31 leap Feb 29", "monthly", sched("UTC", "00:00", 1, 31), "2028-02-29T12:00:00Z", "2028-02-29T00:00:00Z", "2028-03-31T00:00:00Z"},
		{"monthly day 30 on Feb 28", "monthly", sched("UTC", "00:00", 1, 30), "2026-02-28T12:00:00Z", "2026-02-28T00:00:00Z", "2026-03-30T00:00:00Z"},
		{"monthly day 29 non-leap", "monthly", sched("UTC", "00:00", 1, 29), "2026-03-01T00:00:00Z", "2026-02-28T00:00:00Z", "2026-03-29T00:00:00Z"},
		{"monthly day 31 in April", "monthly", sched("UTC", "00:00", 1, 31), "2026-04-30T12:00:00Z", "2026-04-30T00:00:00Z", "2026-05-31T00:00:00Z"},
		{"monthly year rollover", "monthly", sched("UTC", "00:00", 1, 15), "2026-01-03T00:00:00Z", "2025-12-15T00:00:00Z", "2026-01-15T00:00:00Z"},
		// extreme offsets
		{"Kiritimati UTC+14", "daily", sched("Pacific/Kiritimati", "00:00", 1, 1), "2026-10-17T00:00:00Z", "2026-10-16T10:00:00Z", "2026-10-17T10:00:00Z"},
		{"Etc/GMT+12 UTC-12", "daily", sched("Etc/GMT+12", "00:00", 1, 1), "2026-10-17T00:00:00Z", "2026-10-16T12:00:00Z", "2026-10-17T12:00:00Z"},
	}
	for _, c := range cases {
		start, reset := traffic.CalcQuotaWindow(c.period, rule(c.s), ts(c.now))
		if !start.Equal(ts(c.start)) || !reset.Equal(ts(c.resets)) {
			t.Errorf("%s: got %s -> %s, want %s -> %s", c.name, start.UTC().Format(time.RFC3339), reset.UTC().Format(time.RFC3339), c.start, c.resets)
		}
	}
}

// Windows must be built from calendar dates, not 24h steps, and pinned for America/New_York
// so a future Go release cannot change gap/overlap resolution silently.
func TestCalcQuotaWindowDaylightSaving(t *testing.T) {
	ny := sched("America/New_York", "02:30", 1, 1)
	// 2026-03-08 02:30 does not exist (clocks jump 02:00 -> 03:00). Go resolves it with the
	// post-transition offset (EDT), i.e. 06:30Z; that day's window is exactly 24h and the
	// previous one is 23h, with no gap between them.
	start, reset := traffic.CalcQuotaWindow("daily", rule(ny), ts("2026-03-08T12:00:00Z"))
	if !start.Equal(ts("2026-03-08T06:30:00Z")) || !reset.Equal(ts("2026-03-09T06:30:00Z")) {
		t.Errorf("spring-forward gap: got %s -> %s", start.UTC().Format(time.RFC3339), reset.UTC().Format(time.RFC3339))
	}
	// the window before it still closes exactly where this one opens
	pStart, pReset := traffic.CalcQuotaWindow("daily", rule(ny), ts("2026-03-08T05:00:00Z"))
	if !pStart.Equal(ts("2026-03-07T07:30:00Z")) || !pReset.Equal(start) {
		t.Errorf("window before gap: got %s -> %s", pStart.UTC().Format(time.RFC3339), pReset.UTC().Format(time.RFC3339))
	}

	// 2026-11-01 01:30 happens twice (EDT then EST); the first occurrence is used.
	ny2 := sched("America/New_York", "01:30", 1, 1)
	start, reset = traffic.CalcQuotaWindow("daily", rule(ny2), ts("2026-11-01T10:00:00Z"))
	if !start.Equal(ts("2026-11-01T05:30:00Z")) || !reset.Equal(ts("2026-11-02T06:30:00Z")) {
		t.Errorf("fall-back overlap: got %s -> %s", start.UTC().Format(time.RFC3339), reset.UTC().Format(time.RFC3339))
	}
}

func TestCalcQuotaWindowInvariants(t *testing.T) {
	schedules := []*traffic.ResetSchedule{
		sched("UTC", "00:00", 1, 1),
		sched("Asia/Jakarta", "14:00", 3, 31),
		sched("America/New_York", "02:30", 7, 31),
		sched("America/New_York", "01:30", 1, 29),
		sched("Europe/Berlin", "02:30", 5, 30),
		sched("Pacific/Kiritimati", "23:59", 2, 15),
		sched("Etc/GMT+12", "00:00", 4, 1),
	}
	from := ts("2026-01-01T00:00:00Z")
	to := ts("2027-01-02T00:00:00Z")
	for _, s := range schedules {
		for _, period := range []string{"daily", "weekly", "monthly"} {
			for now := from; now.Before(to); now = now.Add(97 * time.Minute) {
				start, reset := traffic.CalcQuotaWindow(period, rule(s), now)
				if start.After(now) || !now.Before(reset) {
					t.Fatalf("%s %s %s: want start <= now < reset, got %s -> %s", s.Timezone, period, now.Format(time.RFC3339), start, reset)
				}
				if !reset.After(start) {
					t.Fatalf("%s %s %s: zero or negative window", s.Timezone, period, now.Format(time.RFC3339))
				}
				nextStart, _ := traffic.CalcQuotaWindow(period, rule(s), reset)
				if !nextStart.Equal(reset) {
					t.Fatalf("%s %s %s: window at reset starts %s, want %s", s.Timezone, period, now.Format(time.RFC3339), nextStart, reset)
				}
			}
		}
	}
}

func TestCalcQuotaWindowUnloadableTimezoneFallsBack(t *testing.T) {
	now := ts("2026-10-07T14:30:00Z")
	bad := sched("Not/AZone", "14:00", 3, 15)
	for _, period := range []string{"daily", "weekly", "monthly"} {
		gs, gr := traffic.CalcQuotaWindow(period, rule(bad), now)
		ws, wr := traffic.CalcQuotaWindow(period, traffic.ResetRule{}, now)
		if !gs.Equal(ws) || !gr.Equal(wr) {
			t.Errorf("%s: got %s -> %s, want default %s -> %s", period, gs, gr, ws, wr)
		}
	}
}

func TestCalcQuotaWindowPinBounds(t *testing.T) {
	pin := traffic.WindowPin{Start: ts("2026-10-01T00:00:00Z"), Until: ts("2026-10-19T17:00:00Z")}
	r := traffic.ResetRule{
		Schedule: sched("Asia/Jakarta", "00:00", 1, 20),
		Pins:     map[string]traffic.WindowPin{"monthly": pin},
	}
	start, reset := traffic.CalcQuotaWindow("monthly", r, pin.Until.Add(-time.Second))
	if !start.Equal(pin.Start) || !reset.Equal(pin.Until) {
		t.Errorf("just before Until: got %s -> %s", start, reset)
	}
	start, reset = traffic.CalcQuotaWindow("monthly", r, pin.Until)
	if !start.Equal(pin.Until) || !reset.Equal(ts("2026-11-19T17:00:00Z")) {
		t.Errorf("at Until: got %s -> %s", start, reset)
	}
	// a pin on one period never touches another
	start, _ = traffic.CalcQuotaWindow("daily", r, ts("2026-10-17T03:00:00Z"))
	if start.Equal(pin.Start) {
		t.Error("monthly pin leaked into daily window")
	}
}
