package timeutil

import (
	"database/sql"
	"testing"
	"time"
)

func TestNormalizeSQLiteTime(t *testing.T) {
	cases := map[string]string{
		"2026-10-06 09:26:40":       "2026-10-06T09:26:40Z",
		"2026-10-06 09:26:40.123":   "2026-10-06T09:26:40Z",
		"2026-10-06T09:26:40":       "2026-10-06T09:26:40Z",
		"2026-10-06T09:26:40Z":      "2026-10-06T09:26:40Z",
		"2026-10-06T16:26:40+07:00": "2026-10-06T09:26:40Z",
		"2026-10-06 09:26:40+00:00": "2026-10-06T09:26:40Z",
		" 2026-10-06 09:26:40 ":     "2026-10-06T09:26:40Z",
		"":                          "",
		"garbage":                   "",
		"2026-13-45 99:00:00":       "",
	}
	for in, want := range cases {
		if got := NormalizeSQLiteTime(in); got != want {
			t.Errorf("NormalizeSQLiteTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNullTimeString(t *testing.T) {
	if NullTimeString(sql.NullString{}) != nil {
		t.Error("NULL should map to nil")
	}
	if NullTimeString(sql.NullString{String: "junk", Valid: true}) != nil {
		t.Error("unparseable should map to nil")
	}
	got := NullTimeString(sql.NullString{String: "2026-10-06 09:26:40", Valid: true})
	if got == nil || *got != "2026-10-06T09:26:40Z" {
		t.Errorf("got %v", got)
	}
}

func TestLoadLocation(t *testing.T) {
	if LoadLocation("").String() != "UTC" || LoadLocation("Not/AZone").String() != "UTC" || LoadLocation("Local").String() != "UTC" {
		t.Error("expected UTC fallback")
	}
	if LoadLocation("Asia/Jakarta").String() != "Asia/Jakarta" {
		t.Error("expected Asia/Jakarta")
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestResolvePeriod(t *testing.T) {
	jkt := mustLoc(t, "Asia/Jakarta")
	ny := mustLoc(t, "America/New_York")
	// 2026-10-06 02:00 UTC = 09:00 in Jakarta, 22:00 previous day in New York.
	now := utc("2026-10-06T02:00:00Z")

	type tc struct {
		name, period, start, end string
		loc                      *time.Location
		now                      time.Time
		cur, prev                Period
	}
	cases := []tc{
		{"today utc", "today", "", "", time.UTC, now,
			Period{From: utc("2026-10-06T00:00:00Z")},
			Period{From: utc("2026-10-05T00:00:00Z"), To: utc("2026-10-06T00:00:00Z")}},
		{"today jakarta", "today", "", "", jkt, now,
			Period{From: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")}},
		{"empty period is today", "", "", "", jkt, now,
			Period{From: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")}},
		{"today new york (local date is still Oct 5)", "today", "", "", ny, now,
			Period{From: utc("2026-10-05T04:00:00Z")},
			Period{From: utc("2026-10-04T04:00:00Z"), To: utc("2026-10-05T04:00:00Z")}},
		{"yesterday jakarta", "yesterday", "", "", jkt, now,
			Period{From: utc("2026-10-04T17:00:00Z"), To: utc("2026-10-05T17:00:00Z")},
			Period{From: utc("2026-10-03T17:00:00Z"), To: utc("2026-10-04T17:00:00Z")}},
		{"7d rolling", "7d", "", "", jkt, now,
			Period{From: utc("2026-09-29T02:00:00Z")},
			Period{From: utc("2026-09-22T02:00:00Z"), To: utc("2026-09-29T02:00:00Z")}},
		{"14d rolling", "14d", "", "", time.UTC, now,
			Period{From: utc("2026-09-22T02:00:00Z")},
			Period{From: utc("2026-09-08T02:00:00Z"), To: utc("2026-09-22T02:00:00Z")}},
		{"30d rolling", "30d", "", "", time.UTC, now,
			Period{From: utc("2026-09-06T02:00:00Z")},
			Period{From: utc("2026-08-07T02:00:00Z"), To: utc("2026-09-06T02:00:00Z")}},
		{"month jakarta", "month", "", "", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"this_month alias", "this_month", "", "", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"last_month jakarta", "last_month", "", "", jkt, now,
			Period{From: utc("2026-08-31T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")},
			Period{From: utc("2026-07-31T17:00:00Z"), To: utc("2026-08-31T17:00:00Z")}},
		{"month in new york is september (local date Oct 5? no: Oct 5 22:00)", "month", "", "", ny, now,
			Period{From: utc("2026-10-01T04:00:00Z")},
			Period{From: utc("2026-09-01T04:00:00Z"), To: utc("2026-10-01T04:00:00Z")}},
		{"all", "all", "", "", jkt, now,
			Period{},
			Period{From: now, To: now}},
		{"custom range jakarta", "custom", "2026-10-01", "2026-10-02", jkt, now,
			Period{From: utc("2026-09-30T17:00:00Z"), To: utc("2026-10-02T17:00:00Z")},
			Period{From: utc("2026-09-28T17:00:00Z"), To: utc("2026-09-30T17:00:00Z")}},
		{"custom reversed dates are swapped", "custom", "2026-10-02", "2026-10-01", time.UTC, now,
			Period{From: utc("2026-10-01T00:00:00Z"), To: utc("2026-10-03T00:00:00Z")},
			Period{From: utc("2026-09-29T00:00:00Z"), To: utc("2026-10-01T00:00:00Z")}},
		{"start only", "", "2026-10-01", "", time.UTC, now,
			Period{From: utc("2026-10-01T00:00:00Z")},
			Period{From: now, To: now}},
		{"end only", "", "", "2026-10-01", time.UTC, now,
			Period{To: utc("2026-10-02T00:00:00Z")},
			Period{From: now, To: now}},
		{"invalid dates fall back to period", "7d", "2026-1-1", "nope", time.UTC, now,
			Period{From: utc("2026-09-29T02:00:00Z")},
			Period{From: utc("2026-09-22T02:00:00Z"), To: utc("2026-09-29T02:00:00Z")}},
		// DST: New York falls back on 2026-11-01 (EDT -4 -> EST -5).
		{"custom across DST new york", "custom", "2026-10-31", "2026-11-01", ny, now,
			Period{From: utc("2026-10-31T04:00:00Z"), To: utc("2026-11-02T05:00:00Z")},
			Period{From: utc("2026-10-29T04:00:00Z"), To: utc("2026-10-31T04:00:00Z")}},
		{"today on DST day new york", "today", "", "", ny, utc("2026-11-01T18:00:00Z"),
			Period{From: utc("2026-11-01T04:00:00Z")},
			Period{From: utc("2026-10-31T04:00:00Z"), To: utc("2026-11-01T04:00:00Z")}},
		{"nil loc is UTC", "today", "", "", nil, now,
			Period{From: utc("2026-10-06T00:00:00Z")},
			Period{From: utc("2026-10-05T00:00:00Z"), To: utc("2026-10-06T00:00:00Z")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur, prev := ResolvePeriod(c.period, c.start, c.end, c.loc, c.now)
			if !cur.From.Equal(c.cur.From) || !cur.To.Equal(c.cur.To) {
				t.Errorf("cur = [%v, %v), want [%v, %v)", cur.From, cur.To, c.cur.From, c.cur.To)
			}
			if !prev.From.Equal(c.prev.From) || !prev.To.Equal(c.prev.To) {
				t.Errorf("prev = [%v, %v), want [%v, %v)", prev.From, prev.To, c.prev.From, c.prev.To)
			}
		})
	}
}

func TestPeriodSQL(t *testing.T) {
	clause, args := Period{}.SQL("timestamp")
	if clause != "1=1" || len(args) != 0 {
		t.Errorf("unbounded: %q %v", clause, args)
	}
	clause, args = Period{From: utc("2026-10-05T17:00:00Z")}.SQL("t.timestamp")
	if clause != "t.timestamp >= datetime(?)" || len(args) != 1 || args[0] != "2026-10-05 17:00:00" {
		t.Errorf("from only: %q %v", clause, args)
	}
	clause, args = Period{From: utc("2026-10-05T17:00:00Z"), To: utc("2026-10-06T17:00:00Z")}.SQL("timestamp")
	if clause != "timestamp >= datetime(?) AND timestamp < datetime(?)" || len(args) != 2 || args[1] != "2026-10-06 17:00:00" {
		t.Errorf("both: %q %v", clause, args)
	}
}

func TestSQLiteOffsetModifier(t *testing.T) {
	if got := SQLiteOffsetModifier(mustLoc(t, "Asia/Jakarta"), utc("2026-10-06T00:00:00Z")); got != "+420 minutes" {
		t.Errorf("jakarta: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "America/New_York"), utc("2026-10-06T00:00:00Z")); got != "-240 minutes" {
		t.Errorf("new york summer: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "America/New_York"), utc("2026-12-06T00:00:00Z")); got != "-300 minutes" {
		t.Errorf("new york winter: %q", got)
	}
	if got := SQLiteOffsetModifier(time.UTC, time.Now()); got != "+0 minutes" {
		t.Errorf("utc: %q", got)
	}
	if got := SQLiteOffsetModifier(mustLoc(t, "Asia/Kolkata"), time.Now()); got != "+330 minutes" {
		t.Errorf("kolkata: %q", got)
	}
}
