package handler_test

import (
	"testing"
	"time"

	"nineguard/internal/handler"
)

func TestRateLimiter_AllowIP(t *testing.T) {
	rl := handler.NewRateLimiter()
	defer rl.Stop()

	now := time.Now()
	rl.SetNow(func() time.Time { return now })

	const ip = "192.0.2.1"

	// 10 requests allowed
	for i := 1; i <= 10; i++ {
		allowed, retryAfter := rl.AllowIP(ip)
		if !allowed || retryAfter != 0 {
			t.Fatalf("Request %d should be allowed, got allowed=%v retryAfter=%d", i, allowed, retryAfter)
		}
	}

	// 11th request denied
	allowed, retryAfter := rl.AllowIP(ip)
	if allowed {
		t.Fatalf("Request 11 should be denied")
	}
	if retryAfter < 1 || retryAfter > 60 {
		t.Fatalf("Expected retryAfter between 1 and 60, got %d", retryAfter)
	}

	// Another IP still allowed
	allowed2, _ := rl.AllowIP("192.0.2.2")
	if !allowed2 {
		t.Fatalf("Different IP should be allowed")
	}

	// Advance time past 1 minute window
	now = now.Add(61 * time.Second)

	// Now original IP allowed again
	allowedAfter, _ := rl.AllowIP(ip)
	if !allowedAfter {
		t.Fatalf("Original IP should be allowed after window expired")
	}
}

func TestRateLimiter_UserLockout(t *testing.T) {
	rl := handler.NewRateLimiter()
	defer rl.Stop()

	now := time.Now()
	rl.SetNow(func() time.Time { return now })

	// First 4 failures do not lock
	for i := 1; i <= 4; i++ {
		justLocked, retryAfter := rl.RecordUserFailure("login", "alice")
		if justLocked || retryAfter != 0 {
			t.Fatalf("Failure %d should not lock", i)
		}
		locked, _ := rl.IsUserLocked("login", "alice")
		if locked {
			t.Fatalf("User should not be locked after %d failures", i)
		}
	}

	// 5th failure triggers lockout
	justLocked, retryAfter := rl.RecordUserFailure("login", "alice")
	if !justLocked {
		t.Fatalf("5th failure should trigger lockout")
	}
	if retryAfter != 15*60 {
		t.Fatalf("Expected retryAfter 900s, got %d", retryAfter)
	}

	// Check IsUserLocked
	locked, remaining := rl.IsUserLocked("login", "alice")
	if !locked || remaining < 1 {
		t.Fatalf("Expected user to be locked, got locked=%v remaining=%d", locked, remaining)
	}

	// Recovery action for same user is NOT locked (independent per action)
	recoveryLocked, _ := rl.IsUserLocked("recovery", "alice")
	if recoveryLocked {
		t.Fatalf("Recovery action should not be locked when login is locked")
	}

	// Advance time 10 minutes (still locked)
	now = now.Add(10 * time.Minute)
	locked, remaining = rl.IsUserLocked("login", "alice")
	if !locked || remaining < 1 || remaining > 5*60+1 {
		t.Fatalf("Expected user to still be locked, remaining=%d", remaining)
	}

	// Advance time past 15 minutes
	now = now.Add(5*time.Minute + 1*time.Second)
	locked, _ = rl.IsUserLocked("login", "alice")
	if locked {
		t.Fatalf("User should be unlocked after 15 minutes")
	}

	// Record success resets state
	rl.RecordUserFailure("login", "bob")
	rl.RecordUserSuccess("login", "bob")
	// 4 failures on bob should not lock
	for i := 1; i <= 4; i++ {
		rl.RecordUserFailure("login", "bob")
	}
	bobLocked, _ := rl.IsUserLocked("login", "bob")
	if bobLocked {
		t.Fatalf("Bob should not be locked after 4 failures following success")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := handler.NewRateLimiter()
	defer rl.Stop()

	now := time.Now()
	rl.SetNow(func() time.Time { return now })

	_, _ = rl.AllowIP("192.0.2.1")
	_, _ = rl.RecordUserFailure("login", "charlie")

	// Advance time past all windows
	now = now.Add(20 * time.Minute)
	rl.Cleanup()

	// charlie's old failure should be gone
	for i := 1; i <= 4; i++ {
		rl.RecordUserFailure("login", "charlie")
	}
	locked, _ := rl.IsUserLocked("login", "charlie")
	if locked {
		t.Fatalf("Charlie should not be locked after cleanup and 4 new failures")
	}
}
