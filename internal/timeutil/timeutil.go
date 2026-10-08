// Package timeutil converts between SQLite text timestamps, API timestamps
// (RFC 3339 UTC) and viewer-local report periods.
package timeutil

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SQLiteLayout is the text format SQLite's CURRENT_TIMESTAMP produces (UTC, no zone).
const SQLiteLayout = "2006-01-02 15:04:05"

var sqliteParseLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
}

// NormalizeSQLiteTime converts SQLite text timestamps ("2006-01-02 15:04:05",
// with or without fractional seconds, "T" separator or zone) to RFC 3339 UTC
// ("2006-01-02T15:04:05Z"). Zoneless input is treated as UTC.
// Returns "" for empty or unparseable input.
func NormalizeSQLiteTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range sqliteParseLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// NullTimeString normalizes a nullable SQLite timestamp for JSON output.
// Returns nil when the value is NULL or unparseable.
func NullTimeString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := NormalizeSQLiteTime(ns.String)
	if v == "" {
		return nil
	}
	return &v
}

// SQLiteTime formats t as a UTC SQLite text timestamp for bound parameters.
func SQLiteTime(t time.Time) string {
	return t.UTC().Format(SQLiteLayout)
}

// LoadLocation resolves an IANA zone name sent by the viewer.
// Empty, "Local", or unknown names fall back to UTC.
func LoadLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" || len(name) > 64 {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Period is a half-open interval [From, To) of UTC instants.
// A zero From or To means the interval is unbounded on that side.
type Period struct {
	From time.Time
	To   time.Time
}

// SQL returns a WHERE fragment restricting col to the period, plus its
// arguments. An unbounded period returns "1=1" and no arguments.
func (p Period) SQL(col string) (string, []any) {
	var conds []string
	var args []any
	if !p.From.IsZero() {
		conds = append(conds, col+" >= datetime(?)")
		args = append(args, SQLiteTime(p.From))
	}
	if !p.To.IsZero() {
		conds = append(conds, col+" < datetime(?)")
		args = append(args, SQLiteTime(p.To))
	}
	if len(conds) == 0 {
		return "1=1", nil
	}
	return strings.Join(conds, " AND "), args
}

// ResolvePeriod returns the current and previous report windows for a period
// name, with calendar boundaries (midnight, first of month) in loc.
//
// Periods: today (default), yesterday, 7d, 14d, 30d, month (alias this_month),
// last_month, all. When start and/or end ("YYYY-MM-DD", local dates) are valid
// they override period: [start 00:00, end+1 day 00:00) in loc.
func ResolvePeriod(period, start, end string, loc *time.Location, now time.Time) (cur, prev Period) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	y, m, d := local.Date()
	midnight := func(addDays int) time.Time {
		return time.Date(y, m, d+addDays, 0, 0, 0, 0, loc).UTC()
	}
	monthStart := func(addMonths int) time.Time {
		return time.Date(y, m+time.Month(addMonths), 1, 0, 0, 0, 0, loc).UTC()
	}
	empty := Period{From: now.UTC(), To: now.UTC()}

	s, sOK := parseDay(start, loc)
	e, eOK := parseDay(end, loc)
	switch {
	case sOK && eOK:
		if e.Before(s) {
			s, e = e, s
		}
		endExcl := e.AddDate(0, 0, 1)
		days := 0
		for t := s; t.Before(endExcl); t = t.AddDate(0, 0, 1) {
			days++
		}
		cur = Period{From: s.UTC(), To: endExcl.UTC()}
		prev = Period{From: s.AddDate(0, 0, -days).UTC(), To: s.UTC()}
		return cur, prev
	case sOK:
		return Period{From: s.UTC()}, empty
	case eOK:
		return Period{To: e.AddDate(0, 0, 1).UTC()}, empty
	}

	switch period {
	case "yesterday":
		return Period{From: midnight(-1), To: midnight(0)}, Period{From: midnight(-2), To: midnight(-1)}
	case "7d", "14d", "30d":
		n := map[string]int{"7d": 7, "14d": 14, "30d": 30}[period]
		span := time.Duration(n) * 24 * time.Hour
		nowUTC := now.UTC()
		return Period{From: nowUTC.Add(-span)}, Period{From: nowUTC.Add(-2 * span), To: nowUTC.Add(-span)}
	case "month", "this_month":
		return Period{From: monthStart(0)}, Period{From: monthStart(-1), To: monthStart(0)}
	case "last_month":
		return Period{From: monthStart(-1), To: monthStart(0)}, Period{From: monthStart(-2), To: monthStart(-1)}
	case "all":
		return Period{}, empty
	default: // "today" and unknown values
		return Period{From: midnight(0)}, Period{From: midnight(-1), To: midnight(0)}
	}
}

func parseDay(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 10 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// SQLiteOffsetModifier returns a strftime modifier such as "+420 minutes" that
// shifts UTC timestamps into loc, using loc's UTC offset at instant at.
// DST changes inside a range may shift buckets after the change by one hour.
func SQLiteOffsetModifier(loc *time.Location, at time.Time) string {
	if loc == nil {
		loc = time.UTC
	}
	_, off := at.In(loc).Zone()
	return fmt.Sprintf("%+d minutes", off/60)
}
