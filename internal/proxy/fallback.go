package proxy

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FallbackAlerter rate-limits warning logs for upstream model fallbacks.
// Throttles duplicate warnings to at most once per interval per (key, model, served).
type FallbackAlerter struct {
	mu       sync.Mutex
	lastWarn map[string]time.Time
	interval time.Duration
}

// NewFallbackAlerter creates an alerter with the given throttle interval (defaults to 10m).
func NewFallbackAlerter(interval time.Duration) *FallbackAlerter {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &FallbackAlerter{
		lastWarn: make(map[string]time.Time),
		interval: interval,
	}
}

// Alert checks if an alert should be logged. If not throttled, it emits slog.Warn and returns true.
func (a *FallbackAlerter) Alert(keyID, keyName, modelID, servedModel string, now time.Time) bool {
	if a == nil {
		return false
	}

	key := fmt.Sprintf("%s|%s|%s", keyID, modelID, servedModel)

	a.mu.Lock()
	defer a.mu.Unlock()

	last, exists := a.lastWarn[key]
	if exists && now.Sub(last) < a.interval {
		return false
	}

	a.lastWarn[key] = now

	// Clean up stale entries older than 2x interval to prevent memory growth
	if len(a.lastWarn) > 100 {
		cutoff := now.Add(-2 * a.interval)
		for k, t := range a.lastWarn {
			if t.Before(cutoff) {
				delete(a.lastWarn, k)
			}
		}
	}

	slog.Warn("model_fallback: model downgraded by upstream",
		"source", "traffic",
		"key", keyName,
		"key_id", keyID,
		"requested", modelID,
		"served", servedModel,
	)

	return true
}
