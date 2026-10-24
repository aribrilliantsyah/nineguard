package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/traffic"
)

func wantPin(t *testing.T, got map[string]traffic.WindowPin, period, start, until string) {
	t.Helper()
	p, ok := got[period]
	if !ok {
		t.Fatalf("%s: no pin, got %v", period, got)
	}
	if !p.Start.Equal(ts(start)) || !p.Until.Equal(ts(until)) {
		t.Errorf("%s: got %s -> %s, want %s -> %s", period, p.Start.UTC().Format(time.RFC3339), p.Until.UTC().Format(time.RFC3339), start, until)
	}
}

// Spec §5.2 worked examples. Old schedule is the UTC default unless stated.
func TestPinsForChangeWorkedExamples(t *testing.T) {
	now := ts("2026-10-17T03:00:00Z") // Oct 17, 10:00 in Jakarta

	// monthly day 1 -> day 20: Oct 1 -> Oct 20 (shorter, once)
	pins := traffic.PinsForChange(traffic.ResetRule{}, sched("Asia/Jakarta", "00:00", 1, 20), now)
	wantPin(t, pins, "monthly", "2026-10-01T00:00:00Z", "2026-10-19T17:00:00Z")
	if s, r := traffic.CalcQuotaWindow("monthly", traffic.ResetRule{Schedule: sched("Asia/Jakarta", "00:00", 1, 20), Pins: pins}, now); !s.Equal(ts("2026-10-01T00:00:00Z")) || !r.Equal(ts("2026-10-19T17:00:00Z")) {
		t.Errorf("window right after: %s -> %s", s, r)
	}
	// ... then the normal window follows, Oct 20 -> Nov 20
	if s, r := traffic.CalcQuotaWindow("monthly", traffic.ResetRule{Schedule: sched("Asia/Jakarta", "00:00", 1, 20), Pins: pins}, ts("2026-10-19T17:00:00Z")); !s.Equal(ts("2026-10-19T17:00:00Z")) || !r.Equal(ts("2026-11-19T17:00:00Z")) {
		t.Errorf("window after pin: %s -> %s", s, r)
	}

	// monthly day 1 -> day 10: Oct 1 -> Nov 10 (longer, once); the passed Oct 10 reset does not fire
	pins = traffic.PinsForChange(traffic.ResetRule{}, sched("Asia/Jakarta", "00:00", 1, 10), now)
	wantPin(t, pins, "monthly", "2026-10-01T00:00:00Z", "2026-11-09T17:00:00Z")

	// daily 00:00 -> 14:00 at 10:00: today 00:00 -> today 14:00
	pins = traffic.PinsForChange(traffic.ResetRule{}, sched("Asia/Jakarta", "14:00", 1, 1), now)
	wantPin(t, pins, "daily", "2026-10-17T00:00:00Z", "2026-10-17T07:00:00Z")

	// daily 14:00 -> 00:00 at 16:00 Jakarta: today 14:00 -> tomorrow 00:00
	old := traffic.ResetRule{Schedule: sched("Asia/Jakarta", "14:00", 1, 1)}
	pins = traffic.PinsForChange(old, sched("Asia/Jakarta", "00:00", 1, 1), ts("2026-10-17T09:00:00Z"))
	wantPin(t, pins, "daily", "2026-10-17T07:00:00Z", "2026-10-17T17:00:00Z")
}

func TestPinsForChangeNoMovementNoPin(t *testing.T) {
	now := ts("2026-10-17T03:00:00Z")
	s := sched("Asia/Jakarta", "14:00", 3, 15)
	if pins := traffic.PinsForChange(traffic.ResetRule{Schedule: s}, sched("Asia/Jakarta", "14:00", 3, 15), now); len(pins) != 0 {
		t.Errorf("unchanged schedule should pin nothing, got %v", pins)
	}
}

func TestPinsForChangeDropsStalePinWhenBoundariesAlign(t *testing.T) {
	now := ts("2026-10-17T03:00:00Z")
	old := traffic.ResetRule{
		Schedule: sched("Asia/Jakarta", "00:00", 1, 20),
		Pins:     map[string]traffic.WindowPin{"monthly": {Start: ts("2026-10-01T00:00:00Z"), Until: ts("2026-10-19T17:00:00Z")}},
	}
	// new schedule is UTC day 1: its window start (Oct 1 00:00Z) equals the pin start
	pins := traffic.PinsForChange(old, sched("UTC", "00:00", 1, 1), now)
	if _, ok := pins["monthly"]; ok {
		t.Errorf("monthly pin should be dropped, got %v", pins)
	}
}

func TestPinsForChangeSecondEditKeepsOriginalStart(t *testing.T) {
	now := ts("2026-10-17T03:00:00Z")
	first := sched("Asia/Jakarta", "00:00", 1, 20)
	firstPins := traffic.PinsForChange(traffic.ResetRule{}, first, now)

	old := traffic.ResetRule{Schedule: first, Pins: firstPins}
	pins := traffic.PinsForChange(old, sched("Asia/Jakarta", "00:00", 1, 25), now)
	wantPin(t, pins, "monthly", "2026-10-01T00:00:00Z", "2026-10-24T17:00:00Z")
}

func TestPinsForChangeIndependentPeriods(t *testing.T) {
	now := ts("2026-10-17T03:00:00Z")
	pins := traffic.PinsForChange(traffic.ResetRule{}, sched("Asia/Jakarta", "14:00", 3, 20), now)
	d, okD := pins["daily"]
	m, okM := pins["monthly"]
	if !okD || !okM {
		t.Fatalf("want daily and monthly pins, got %v", pins)
	}
	if d.Start.Equal(m.Start) || d.Until.Equal(m.Until) {
		t.Errorf("pins should be independent: daily %v monthly %v", d, m)
	}
	for period, p := range pins {
		if p.Start.After(now) || !now.Before(p.Until) {
			t.Errorf("%s pin must satisfy Start <= now < Until: %v", period, p)
		}
	}
}
