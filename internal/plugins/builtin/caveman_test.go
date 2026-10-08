package builtin

import (
	"strings"
	"testing"
)

func TestApplyCaveman_InjectionAndIdempotency(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "How do I reverse a string in Go?"},
	}
	out, skipped, _, overhead, err := ApplyCaveman(msgs, map[string]any{"variant": "caveman"})
	if err != nil || skipped {
		t.Fatalf("unexpected err or skip: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0]["role"] != "system" {
		t.Fatalf("expected system message at index 0")
	}
	sysContent, _ := out[0]["content"].(string)
	if !strings.HasPrefix(sysContent, "[nineguard:caveman]\n") {
		t.Fatalf("missing marker prefix: %s", sysContent)
	}
	if overhead <= 0 {
		t.Fatalf("expected positive overhead, got %d", overhead)
	}

	// Test idempotency when marker present
	out2, skipped2, reason, overhead2, err2 := ApplyCaveman(out, map[string]any{"variant": "caveman"})
	if err2 != nil || !skipped2 || reason != "marker_present" || overhead2 != 0 {
		t.Fatalf("expected idempotency skip, got skipped=%v, reason=%s, err=%v", skipped2, reason, err2)
	}
	if len(out2) != len(out) {
		t.Fatalf("expected message list unchanged on skip")
	}
}

func TestApplyCaveman_DeveloperRoleMarkerDetection(t *testing.T) {
	msgs := []map[string]any{
		{"role": "developer", "content": "[nineguard:caveman]\nexisting prompt"},
		{"role": "user", "content": "hello"},
	}
	out, skipped, reason, overhead, err := ApplyCaveman(msgs, nil)
	if err != nil || !skipped || reason != "marker_present" || overhead != 0 {
		t.Fatalf("expected marker detection in developer role, got skipped=%v, reason=%s", skipped, reason)
	}
	if len(out) != 2 {
		t.Fatalf("expected untouched message count")
	}
}

func TestApplyCaveman_PromptOverride(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hi"},
	}
	custom := "Custom terse style."
	out, skipped, _, _, err := ApplyCaveman(msgs, map[string]any{"prompt_override": custom})
	if err != nil || skipped {
		t.Fatalf("unexpected err: %v", err)
	}
	sysContent, _ := out[0]["content"].(string)
	if !strings.Contains(sysContent, custom) {
		t.Fatalf("expected custom prompt to be injected, got %s", sysContent)
	}
}
