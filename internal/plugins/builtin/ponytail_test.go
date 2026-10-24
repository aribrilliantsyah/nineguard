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

func TestApplyPonytailToMessages(t *testing.T) {
	// 1. Omitted system -> creates string system
	body1 := map[string]any{
		"model": "claude-3-7-sonnet",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out1, skipped1, _, overhead1, err1 := ApplyPonytailToMessages(body1, nil)
	if err1 != nil || skipped1 {
		t.Fatalf("unexpected err or skip: %v", err1)
	}
	sys1, ok := out1["system"].(string)
	if !ok || !strings.HasPrefix(sys1, "[nineguard:ponytail]\n") {
		t.Fatalf("expected system string with marker, got %v", out1["system"])
	}
	if overhead1 <= 0 {
		t.Fatalf("expected positive overhead, got %d", overhead1)
	}

	// 2. Existing string system -> prepends
	body2 := map[string]any{
		"system": "Original prompt.",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out2, skipped2, _, _, err2 := ApplyPonytailToMessages(body2, nil)
	if err2 != nil || skipped2 {
		t.Fatalf("unexpected err or skip: %v", err2)
	}
	sys2, _ := out2["system"].(string)
	if !strings.HasPrefix(sys2, "[nineguard:ponytail]\n") || !strings.HasSuffix(sys2, "Original prompt.") {
		t.Fatalf("expected prepended system string, got %q", sys2)
	}

	// 3. Array of content blocks
	body3 := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "Block prompt"},
		},
	}
	out3, skipped3, _, _, err3 := ApplyPonytailToMessages(body3, nil)
	if err3 != nil || skipped3 {
		t.Fatalf("unexpected err or skip: %v", err3)
	}
	sysArr, ok := out3["system"].([]any)
	if !ok || len(sysArr) != 2 {
		t.Fatalf("expected system array of length 2, got %v", out3["system"])
	}

	// 4. Idempotency when marker present
	_, skipped4, reason4, overhead4, err4 := ApplyPonytailToMessages(out3, nil)
	if err4 != nil || !skipped4 || reason4 != "marker_present" || overhead4 != 0 {
		t.Fatalf("expected skip due to marker_present, got skipped=%v, reason=%q, err=%v", skipped4, reason4, err4)
	}
}
