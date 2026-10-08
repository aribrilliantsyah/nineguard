package builtin_test

import (
	"strings"
	"testing"

	"nineguard/internal/plugins/builtin"
)

func TestSecretGuardDetectionAndBlock(t *testing.T) {
	messages := []map[string]any{
		{
			"role":    "user",
			"content": "Here is my key: sk-abcdef1234567890abcdef1234567890, please debug it.",
		},
	}

	// Default action = block
	_, skipped, rejected, rejectMsg, err := builtin.ApplySecretGuard(messages, map[string]any{"action": "block"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Errorf("expected not skipped")
	}
	if !rejected {
		t.Fatalf("expected request to be rejected")
	}
	if !strings.Contains(rejectMsg, "SecretGuard") {
		t.Errorf("expected reject message to mention SecretGuard, got %s", rejectMsg)
	}
}

func TestSecretGuardRedact(t *testing.T) {
	messages := []map[string]any{
		{
			"role":    "user",
			"content": "My AWS key is AKIAIOSFODNN7EXAMPLE and secret is AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		},
	}

	newMsgs, skipped, rejected, _, err := builtin.ApplySecretGuard(messages, map[string]any{"action": "redact"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped || rejected {
		t.Fatalf("expected redacted, not skipped (%v) or rejected (%v)", skipped, rejected)
	}

	content, ok := newMsgs[0]["content"].(string)
	if !ok {
		t.Fatalf("expected string content")
	}

	if strings.Contains(content, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("expected API key to be redacted, got %s", content)
	}
	if !strings.Contains(content, "[REDACTED_SECRET:") {
		t.Errorf("expected redacted placeholder, got %s", content)
	}
}

func TestSecretGuardCleanContentPasses(t *testing.T) {
	messages := []map[string]any{
		{
			"role":    "user",
			"content": "Hello, write a binary search algorithm in Go.",
		},
	}

	_, skipped, rejected, _, err := builtin.ApplySecretGuard(messages, map[string]any{"action": "block"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skipped {
		t.Errorf("expected skipped when no secrets present")
	}
	if rejected {
		t.Errorf("expected not rejected")
	}
}
