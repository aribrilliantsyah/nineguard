package traffic

import "time"

// WindowPin is a Transitional Window for one Quota Period (ADR 0010 #5): the window running
// when the schedule changed keeps its Start and ends at Until, the first reset of the new schedule.
type WindowPin struct {
	Start time.Time `json:"start"`
	Until time.Time `json:"until"`
}

// ResetRule is everything window math needs for one key: its Effective Reset Schedule
// (nil = ADR 0005 default) and its stored Transitional Windows by Quota Period.
// The zero ResetRule behaves exactly like the pre-schedule code.
type ResetRule struct {
	Schedule *ResetSchedule
	Pins     map[string]WindowPin
}

// scheduledPeriods are the Quota Periods that reset and can therefore be pinned.
var scheduledPeriods = []string{"daily", "weekly", "monthly"}

// PinsForChange returns the Transitional Windows a key needs when its Effective Reset Schedule
// changes from oldRule to newSchedule at now. oldRule's own pins count, so editing again during
// a Transitional Window keeps its original start. A period whose window start does not move
// gets no pin.
func PinsForChange(oldRule ResetRule, newSchedule *ResetSchedule, now time.Time) map[string]WindowPin {
	pins := map[string]WindowPin{}
	for _, period := range scheduledPeriods {
		oldStart, _ := CalcQuotaWindow(period, oldRule, now)
		newStart, newReset := scheduleWindow(period, newSchedule, now)
		if oldStart.Equal(newStart) {
			continue
		}
		pins[period] = WindowPin{Start: oldStart, Until: newReset}
	}
	return pins
}
