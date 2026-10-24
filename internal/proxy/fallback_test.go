package proxy_test

import (
	"testing"
	"time"

	"nineguard/internal/proxy"
)

func TestFallbackAlerter(t *testing.T) {
	alerter := proxy.NewFallbackAlerter(10 * time.Minute)
	now := time.Now()

	// First alert should fire
	alerted := alerter.Alert("key-1", "test-agent", "9router/claude-opus-4.5", "claude-sonnet-4-5-20250929", now)
	if !alerted {
		t.Errorf("expected first alert to fire")
	}

	// Immediate duplicate within 10m should be throttled
	alertedAgain := alerter.Alert("key-1", "test-agent", "9router/claude-opus-4.5", "claude-sonnet-4-5-20250929", now.Add(2*time.Minute))
	if alertedAgain {
		t.Errorf("expected duplicate alert at +2m to be throttled")
	}

	// Different key should fire
	alertedDiffKey := alerter.Alert("key-2", "other-agent", "9router/claude-opus-4.5", "claude-sonnet-4-5-20250929", now.Add(3*time.Minute))
	if !alertedDiffKey {
		t.Errorf("expected alert for different key to fire")
	}

	// Different served model should fire
	alertedDiffServed := alerter.Alert("key-1", "test-agent", "9router/claude-opus-4.5", "claude-haiku-4-5", now.Add(4*time.Minute))
	if !alertedDiffServed {
		t.Errorf("expected alert for different served model to fire")
	}

	// After 10 minutes interval, original alert should fire again
	alertedAfter10m := alerter.Alert("key-1", "test-agent", "9router/claude-opus-4.5", "claude-sonnet-4-5-20250929", now.Add(11*time.Minute))
	if !alertedAfter10m {
		t.Errorf("expected alert to fire after 11m (>10m interval)")
	}
}
