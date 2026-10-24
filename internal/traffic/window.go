package traffic

import (
	"log/slog"
	"sync"
	"time"
)

var windowFallbackLog sync.Once

// scheduleWindow is the pure calendar part of CalcQuotaWindow (no pins). A nil or unusable
// schedule, and the periods that never reset, use the ADR 0005 UTC calendar.
//
// Every reset instant S(k) comes from one function (nth), and start and reset are taken as
// consecutive values of it, so consecutive windows share a boundary exactly: no gap, no overlap.
// Instants are built from local dates with time.Date, never by adding 24h, so windows do not
// drift an hour across daylight saving changes. A local time skipped or repeated by a daylight
// saving change resolves however time.Date resolves it; the tests pin that result.
func scheduleWindow(period string, sched *ResetSchedule, now time.Time) (start, resetAt time.Time) {
	loc, ok := sched.load()
	if !ok {
		return defaultWindow(period, now)
	}
	minute, ok := ParseClock(sched.Time)
	if !ok {
		return defaultWindow(period, now)
	}
	hour, min := minute/60, minute%60
	local := now.In(loc)
	ly, lm, ld := local.Date()

	var nth func(k int) time.Time
	switch period {
	case "daily":
		nth = func(k int) time.Time {
			return time.Date(ly, lm, ld+k, hour, min, 0, 0, loc)
		}
	case "weekly":
		wd := int(local.Weekday()) // Sunday = 0
		if wd == 0 {
			wd = 7
		}
		mondayDay := ld - (wd - 1)
		weekday := clampInt(sched.Weekday, 1, 7)
		nth = func(k int) time.Time {
			return time.Date(ly, lm, mondayDay+7*k+(weekday-1), hour, min, 0, 0, loc)
		}
	case "monthly":
		nth = func(k int) time.Time {
			first := time.Date(ly, time.Month(int(lm)+k), 1, 0, 0, 0, 0, time.UTC)
			y, mo := first.Year(), first.Month()
			day := clampInt(sched.Day, 1, daysIn(y, mo))
			return time.Date(y, mo, day, hour, min, 0, 0, loc)
		}
	default:
		return defaultWindow(period, now)
	}

	k := 0
	for i := 0; i < 4 && nth(k).After(now); i++ {
		k--
	}
	for i := 0; i < 4 && !nth(k+1).After(now); i++ {
		k++
	}
	start, resetAt = nth(k).UTC(), nth(k+1).UTC()

	if start.After(now) || !now.Before(resetAt) || !resetAt.After(start) {
		windowFallbackLog.Do(func() {
			slog.Warn("quota reset schedule produced an invalid window; using the UTC default",
				"timezone", sched.Timezone, "period", period)
		})
		return defaultWindow(period, now)
	}
	return start, resetAt
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
