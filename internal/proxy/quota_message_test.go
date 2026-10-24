package proxy

import (
	"strings"
	"testing"
	"time"

	"nineguard/internal/traffic"
)

func TestQuotaExceededBody(t *testing.T) {
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)

	body, ra := quotaExceededBody("9router/claude-opus-4", 1004211, 1000000, "daily", traffic.ResetRule{}, reset, now)
	s := string(body)
	for _, want := range []string{`"model_quota_exceeded"`, `"param": "model"`, "1,004,211 / 1,000,000", "insufficient_quota", "Resets in 6h 0m"} {
		if !strings.Contains(s, want) {
			t.Errorf("model quota body missing %q:\n%s", want, s)
		}
	}
	if ra != 6*3600 {
		t.Errorf("retryAfter=%d", ra)
	}

	body, ra = quotaExceededBody("", 120, 100, "daily", traffic.ResetRule{}, reset, now)
	s = string(body)
	if !strings.Contains(s, `"quota_exceeded"`) || strings.Contains(s, `"param"`) || strings.Contains(s, "model_quota_exceeded") || ra < 1 {
		t.Errorf("key quota body: %s ra=%d", s, ra)
	}

	for _, id := range []string{"", "p/a"} {
		body, ra = quotaExceededBody(id, 9, 5, "total", traffic.ResetRule{}, time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), now)
		if ra != 0 || !strings.Contains(string(body), "does not reset") {
			t.Errorf("total (%q): ra=%d %s", id, ra, body)
		}
	}

	// A model id with quotes must still be valid JSON.
	body, _ = quotaExceededBody(`we"ird`, 2, 1, "daily", traffic.ResetRule{}, reset, now)
	if !strings.Contains(string(body), `we\"ird`) {
		t.Errorf("model id not escaped: %s", body)
	}
}
