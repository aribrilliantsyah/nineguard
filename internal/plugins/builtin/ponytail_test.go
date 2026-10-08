package builtin

import (
	"strings"
	"testing"
)

func TestApplyPonytail_InjectionAndIdempotency(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "How do I optimize SQL queries?"},
	}
	out, skipped, _, overhead, err := ApplyPonytail(msgs, map[string]any{"level": "full"})
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
	if !strings.HasPrefix(sysContent, "[nineguard:ponytail]\n") {
		t.Fatalf("missing marker prefix: %s", sysContent)
	}
	if overhead <= 0 {
		t.Fatalf("expected positive overhead, got %d", overhead)
	}

	// Idempotency check
	out2, skipped2, reason, overhead2, err2 := ApplyPonytail(out, map[string]any{"level": "full"})
	if err2 != nil || !skipped2 || reason != "marker_present" || overhead2 != 0 {
		t.Fatalf("expected idempotency skip, got skipped=%v, reason=%s, err=%v", skipped2, reason, err2)
	}
	if len(out2) != len(out) {
		t.Fatalf("expected message list unchanged on skip")
	}
}
