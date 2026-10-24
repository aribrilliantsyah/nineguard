package traffic

import (
	"fmt"
	"time"
)

// ResetSchedule says when Quota Periods reset (ADR 0010). A nil *ResetSchedule means the
// ADR 0005 default: 00:00 UTC, Monday, the 1st.
type ResetSchedule struct {
	Timezone string `json:"timezone"` // IANA name
	Time     string `json:"time"`     // "HH:MM", 24h
	Weekday  int    `json:"weekday"`  // 1 = Monday .. 7 = Sunday
	Day      int    `json:"day"`      // 1..31; days past the end of a short month mean its last day
}

// Validate checks every field. The error text names the offending field. Unlike
// timeutil.LoadLocation (viewer display, silent UTC fallback) a stored enforcement
// setting must surface a typo, so "Local" and unknown names are rejected.
func (s *ResetSchedule) Validate() error {
	if s == nil {
		return fmt.Errorf("schedule is required")
	}
	if s.Timezone == "" || len(s.Timezone) > 64 || s.Timezone == "Local" {
		return fmt.Errorf("timezone must be a valid IANA name (e.g. Asia/Jakarta)")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("timezone %q is not a known IANA name", s.Timezone)
	}
	if _, ok := ParseClock(s.Time); !ok {
		return fmt.Errorf("time must be HH:MM between 00:00 and 23:59")
	}
	if s.Weekday < 1 || s.Weekday > 7 {
		return fmt.Errorf("weekday must be between 1 (Monday) and 7 (Sunday)")
	}
	if s.Day < 1 || s.Day > 31 {
		return fmt.Errorf("day must be between 1 and 31")
	}
	return nil
}

// Location is the schedule's zone, or UTC when s is nil or the name no longer loads.
func (s *ResetSchedule) Location() *time.Location {
	loc, _ := s.load()
	return loc
}

// load returns the zone and whether the schedule is usable for window math.
func (s *ResetSchedule) load() (*time.Location, bool) {
	if s == nil {
		return time.UTC, false
	}
	if s.Timezone == "" || s.Timezone == "Local" || len(s.Timezone) > 64 {
		return time.UTC, false
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC, false
	}
	return loc, true
}

// Equal reports whether two schedules hold the same values (nil == nil).
func (s *ResetSchedule) Equal(o *ResetSchedule) bool {
	if s == nil || o == nil {
		return s == nil && o == nil
	}
	return *s == *o
}

// ParseClock reads a strict "HH:MM" into minutes after midnight.
func ParseClock(s string) (minute int, ok bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	digit := func(c byte) (int, bool) {
		if c < '0' || c > '9' {
			return 0, false
		}
		return int(c - '0'), true
	}
	h1, ok1 := digit(s[0])
	h2, ok2 := digit(s[1])
	m1, ok3 := digit(s[3])
	m2, ok4 := digit(s[4])
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return 0, false
	}
	h, m := h1*10+h2, m1*10+m2
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// FormatClock renders minutes after midnight as "HH:MM".
func FormatClock(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}
