package traffic_test

import (
	"strings"
	"testing"
	"time"

	"nineguard/internal/traffic"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func sched(tz, clock string, weekday, day int) *traffic.ResetSchedule {
	return &traffic.ResetSchedule{Timezone: tz, Time: clock, Weekday: weekday, Day: day}
}

func TestResetScheduleValidate(t *testing.T) {
	if err := sched("Asia/Jakarta", "14:00", 3, 31).Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	cases := []struct {
		name string
		s    *traffic.ResetSchedule
		want string
	}{
		{"empty tz", sched("", "00:00", 1, 1), "timezone"},
		{"Local", sched("Local", "00:00", 1, 1), "timezone"},
		{"unknown tz", sched("Not/AZone", "00:00", 1, 1), "timezone"},
		{"long tz", sched(strings.Repeat("a", 65), "00:00", 1, 1), "timezone"},
		{"hour 24", sched("UTC", "24:00", 1, 1), "time"},
		{"short clock", sched("UTC", "9:5", 1, 1), "time"},
		{"letters", sched("UTC", "ab", 1, 1), "time"},
		{"minute 60", sched("UTC", "10:60", 1, 1), "time"},
		{"weekday 0", sched("UTC", "00:00", 0, 1), "weekday"},
		{"weekday 8", sched("UTC", "00:00", 8, 1), "weekday"},
		{"day 0", sched("UTC", "00:00", 1, 0), "day"},
		{"day 32", sched("UTC", "00:00", 1, 32), "day"},
	}
	for _, c := range cases {
		err := c.s.Validate()
		if err == nil {
			t.Errorf("%s: expected error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q should name %q", c.name, err, c.want)
		}
	}
}

func TestResetScheduleLocation(t *testing.T) {
	var nilSched *traffic.ResetSchedule
	if nilSched.Location() != time.UTC {
		t.Error("nil schedule should be UTC")
	}
	if sched("Not/AZone", "00:00", 1, 1).Location() != time.UTC {
		t.Error("unloadable timezone should fall back to UTC")
	}
	if got := sched("Asia/Jakarta", "00:00", 1, 1).Location().String(); got != "Asia/Jakarta" {
		t.Errorf("got %s", got)
	}
}

func TestResetScheduleEqual(t *testing.T) {
	var a, b *traffic.ResetSchedule
	if !a.Equal(b) {
		t.Error("nil == nil")
	}
	a = sched("UTC", "00:00", 1, 1)
	if a.Equal(nil) {
		t.Error("non-nil != nil")
	}
	if !a.Equal(sched("UTC", "00:00", 1, 1)) {
		t.Error("same values should be equal")
	}
	if a.Equal(sched("UTC", "00:01", 1, 1)) {
		t.Error("different time should differ")
	}
}

func TestClockHelpers(t *testing.T) {
	if m, ok := traffic.ParseClock("14:05"); !ok || m != 14*60+5 {
		t.Errorf("ParseClock(14:05) = %d %v", m, ok)
	}
	for _, bad := range []string{"", "24:00", "9:05", "09:5", "09-05", "aa:bb", "23:60", "-1:00"} {
		if _, ok := traffic.ParseClock(bad); ok {
			t.Errorf("ParseClock(%q) should fail", bad)
		}
	}
	if traffic.FormatClock(0) != "00:00" || traffic.FormatClock(14*60+5) != "14:05" || traffic.FormatClock(1439) != "23:59" {
		t.Error("FormatClock mismatch")
	}
}
