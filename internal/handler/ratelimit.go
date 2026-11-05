package handler

import (
	"math"
	"strings"
	"sync"
	"time"
)

const (
	ipRateLimitWindow  = 1 * time.Minute
	ipRateLimitMax     = 10
	userLockoutWindow  = 15 * time.Minute
	userLockoutMaxFail = 5
)

type userLockState struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

type RateLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	ipRequests  map[string][]time.Time
	userLocks   map[string]*userLockState
	stopCleanup chan struct{}
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		now:         time.Now,
		ipRequests:  make(map[string][]time.Time),
		userLocks:   make(map[string]*userLockState),
		stopCleanup: make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) Stop() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.stopCleanup != nil {
		close(rl.stopCleanup)
		rl.stopCleanup = nil
	}
}

func (rl *RateLimiter) SetNow(fn func() time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if fn == nil {
		rl.now = time.Now
	} else {
		rl.now = fn
	}
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.Cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()

	cutoff := now.Add(-ipRateLimitWindow)
	for ip, timestamps := range rl.ipRequests {
		var valid []time.Time
		for _, t := range timestamps {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.ipRequests, ip)
		} else {
			rl.ipRequests[ip] = valid
		}
	}

	for key, state := range rl.userLocks {
		if !now.Before(state.lockedUntil) && now.Sub(state.lastSeen) > userLockoutWindow {
			delete(rl.userLocks, key)
		}
	}
}

func (rl *RateLimiter) AllowIP(ip string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	cutoff := now.Add(-ipRateLimitWindow)

	timestamps := rl.ipRequests[ip]
	var valid []time.Time
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= ipRateLimitMax {
		oldest := valid[0]
		remaining := oldest.Add(ipRateLimitWindow).Sub(now)
		secs := int(math.Ceil(remaining.Seconds()))
		if secs < 1 {
			secs = 1
		}
		rl.ipRequests[ip] = valid
		return false, secs
	}

	valid = append(valid, now)
	rl.ipRequests[ip] = valid
	return true, 0
}

func userLockKey(action, username string) string {
	return strings.ToLower(strings.TrimSpace(action)) + ":" + strings.ToLower(strings.TrimSpace(username))
}

func (rl *RateLimiter) IsUserLocked(action, username string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := userLockKey(action, username)
	state, exists := rl.userLocks[key]
	if !exists {
		return false, 0
	}

	now := rl.now()
	if now.Before(state.lockedUntil) {
		remaining := state.lockedUntil.Sub(now)
		secs := int(math.Ceil(remaining.Seconds()))
		if secs < 1 {
			secs = 1
		}
		return true, secs
	}

	if !state.lockedUntil.IsZero() {
		state.failures = 0
		state.lockedUntil = time.Time{}
	}
	return false, 0
}

func (rl *RateLimiter) RecordUserFailure(action, username string) (justLocked bool, retryAfter int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := userLockKey(action, username)
	state, exists := rl.userLocks[key]
	now := rl.now()
	if !exists {
		state = &userLockState{}
		rl.userLocks[key] = state
	}

	state.lastSeen = now

	if now.Before(state.lockedUntil) {
		remaining := state.lockedUntil.Sub(now)
		secs := int(math.Ceil(remaining.Seconds()))
		if secs < 1 {
			secs = 1
		}
		return true, secs
	}

	if !state.lockedUntil.IsZero() {
		state.failures = 0
		state.lockedUntil = time.Time{}
	}

	state.failures++
	if state.failures >= userLockoutMaxFail {
		state.lockedUntil = now.Add(userLockoutWindow)
		return true, int(userLockoutWindow.Seconds())
	}

	return false, 0
}

func (rl *RateLimiter) RecordUserSuccess(action, username string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := userLockKey(action, username)
	delete(rl.userLocks, key)
}
