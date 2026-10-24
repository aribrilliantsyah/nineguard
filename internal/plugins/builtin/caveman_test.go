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

func TestApplyCavemanToMessages(t *testing.T) {
	// 1. Omitted system -> creates string system
	body1 := map[string]any{
		"model": "claude-3-7-sonnet",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out1, skipped1, _, overhead1, err1 := ApplyCavemanToMessages(body1, nil)
	if err1 != nil || skipped1 {
		t.Fatalf("unexpected err or skip: %v", err1)
	}
	sys1, ok := out1["system"].(string)
	if !ok || !strings.HasPrefix(sys1, "[nineguard:caveman]\n") {
		t.Fatalf("expected system string with marker, got %v", out1["system"])
	}
	if overhead1 <= 0 {
		t.Fatalf("expected positive overhead, got %d", overhead1)
	}

	// 2. Existing string system -> prepends
	body2 := map[string]any{
		"system": "You are a helpful coding assistant.",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out2, skipped2, _, _, err2 := ApplyCavemanToMessages(body2, nil)
	if err2 != nil || skipped2 {
		t.Fatalf("unexpected err or skip: %v", err2)
	}
	sys2, _ := out2["system"].(string)
	if !strings.HasPrefix(sys2, "[nineguard:caveman]\n") || !strings.HasSuffix(sys2, "You are a helpful coding assistant.") {
		t.Fatalf("expected prepended system string, got %q", sys2)
	}

	// 3. Array of content blocks with cache_control -> prepends text block at index 0, keeps cache_control
	body3 := map[string]any{
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "Base instructions",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out3, skipped3, _, _, err3 := ApplyCavemanToMessages(body3, nil)
	if err3 != nil || skipped3 {
		t.Fatalf("unexpected err or skip: %v", err3)
	}
	sysArr, ok := out3["system"].([]any)
	if !ok || len(sysArr) != 2 {
		t.Fatalf("expected system array of length 2, got %v", out3["system"])
	}
	firstBlock, _ := sysArr[0].(map[string]any)
	firstText, _ := firstBlock["text"].(string)
	if !strings.HasPrefix(firstText, "[nineguard:caveman]\n") {
		t.Fatalf("expected marker in first block text, got %q", firstText)
	}
	secondBlock, _ := sysArr[1].(map[string]any)
	if secondBlock["text"] != "Base instructions" || secondBlock["cache_control"] == nil {
		t.Fatalf("expected preserved second block with cache_control, got %v", secondBlock)
	}

	// 4. Idempotency when marker present
	_, skipped4, reason4, overhead4, err4 := ApplyCavemanToMessages(out3, nil)
	if err4 != nil || !skipped4 || reason4 != "marker_present" || overhead4 != 0 {
		t.Fatalf("expected skip due to marker_present, got skipped=%v, reason=%q, err=%v", skipped4, reason4, err4)
	}
}

