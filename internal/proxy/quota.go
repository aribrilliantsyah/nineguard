package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"nineguard/internal/traffic"
)

// quotaExceededBody builds the 429 JSON for a Key Quota (modelID == "") or a Model Quota
// (ADR 0005, ADR 0007). retryAfter is 0 for a lifetime ("total") quota, which never resets:
// the caller must then omit the Retry-After header. The reset time is shown in the timezone
// of the key's Effective Reset Schedule, UTC when it has none (ADR 0010).
func quotaExceededBody(modelID string, consumed, limit int64, period string, rule traffic.ResetRule, resetAt, now time.Time) (body []byte, retryAfter int) {
	return quotaExceededBodyForFormat("", modelID, consumed, limit, period, rule, resetAt, now)
}

func quotaExceededBodyForFormat(format, modelID string, consumed, limit int64, period string, rule traffic.ResetRule, resetAt, now time.Time) (body []byte, retryAfter int) {
	prefix := "API key token quota exceeded"
	code := "quota_exceeded"
	if modelID != "" {
		prefix = fmt.Sprintf("Model quota exceeded for '%s'", modelID)
		code = "model_quota_exceeded"
	}

	var msg string
	if period == "total" {
		msg = fmt.Sprintf("%s (%s / %s tokens total). Lifetime quota, does not reset.", prefix, formatTokenCount(consumed), formatTokenCount(limit))
	} else {
		until := resetAt.Sub(now)
		atDisplay := resetAtDisplay(period, rule.Schedule, resetAt)
		msg = fmt.Sprintf("%s (%s / %s tokens %s). Resets in %s (at %s).", prefix, formatTokenCount(consumed), formatTokenCount(limit), period, formatUntil(until), atDisplay)

		retryAfter = int(until.Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
	}

	if format == "messages" {
		errObj := map[string]any{"type": "rate_limit_error", "message": msg}
		body, _ = json.MarshalIndent(map[string]any{"type": "error", "error": errObj}, "", "\t")
		return body, retryAfter
	}

	errObj := map[string]any{"message": msg, "type": "insufficient_quota", "code": code}
	if modelID != "" {
		errObj["param"] = "model"
	}
	body, _ = json.MarshalIndent(map[string]any{"error": errObj}, "", "\t")
	return body, retryAfter
}

// formatUntil renders a remaining duration: "45m", "3h 5m", and from 24h on "2d 5h".
func formatUntil(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	switch {
	case hours >= 24:
		return fmt.Sprintf("%dd %dh", hours/24, hours%24)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// resetAtDisplay is the clock time of the reset in the schedule's zone ("14:00 WIB"), with the
// date for weekly and monthly periods ("14:00 WIB on 02 Nov").
func resetAtDisplay(period string, sched *traffic.ResetSchedule, resetAt time.Time) string {
	tzName := "UTC"
	if sched != nil {
		tzName = sched.Timezone
	}
	local := resetAt.In(sched.Location())
	at := local.Format("15:04") + " " + zoneLabel(local, tzName)
	if period == "weekly" || period == "monthly" {
		at += " on " + local.Format("02 Jan")
	}
	return at
}

// zoneLabel is the zone abbreviation Go reports for t ("WIB", "EDT"). Zones that only have a
// numeric abbreviation (Go returns "+04") use the IANA name instead.
func zoneLabel(t time.Time, ianaName string) string {
	abbr, _ := t.Zone()
	if abbr == "" {
		return ianaName
	}
	for _, r := range abbr {
		if r < 'A' || r > 'Z' {
			return ianaName
		}
	}
	return abbr
}

// writeQuotaExceeded sends the 429 and returns its message for the traffic log.
func writeQuotaExceeded(w http.ResponseWriter, format, modelID string, consumed, limit int64, period string, rule traffic.ResetRule, resetAt time.Time) {
	body, retryAfter := quotaExceededBodyForFormat(format, modelID, consumed, limit, period, rule, resetAt, time.Now())
	w.Header().Set("Content-Type", "application/json")
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write(body)
}
