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

func TestSecretGuardToMessages_Block(t *testing.T) {
	// 1. Secret in top-level system string
	body1 := map[string]any{
		"system": "My secret is GITHUB_TOKEN=ghp_123456789012345678901234567890123456",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	_, skipped1, rejected1, rejectMsg1, err1 := builtin.ApplySecretGuardToMessages(body1, map[string]any{"action": "block"})
	if err1 != nil || skipped1 || !rejected1 {
		t.Fatalf("expected rejected on secret in system string, got skipped=%v, rejected=%v, err=%v", skipped1, rejected1, err1)
	}
	if !strings.Contains(rejectMsg1, "SecretGuard") {
		t.Errorf("expected reject message to mention SecretGuard, got %s", rejectMsg1)
	}

	// 2. Secret inside tool_result content
	body2 := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":    "tool_result",
						"content": "output contains DATABASE_URL=postgres://user:pass@localhost/db",
					},
				},
			},
		},
	}
	_, skipped2, rejected2, _, err2 := builtin.ApplySecretGuardToMessages(body2, map[string]any{"action": "block"})
	if err2 != nil || skipped2 || !rejected2 {
		t.Fatalf("expected rejected on secret in tool_result, got skipped=%v, rejected=%v, err=%v", skipped2, rejected2, err2)
	}

	// 3. Secret inside tool_use input
	body3 := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use",
						"input": map[string]any{
							"token": "ghp_123456789012345678901234567890123456",
						},
					},
				},
			},
		},
	}
	_, skipped3, rejected3, _, err3 := builtin.ApplySecretGuardToMessages(body3, map[string]any{"action": "block"})
	if err3 != nil || skipped3 || !rejected3 {
		t.Fatalf("expected rejected on secret in tool_use input, got skipped=%v, rejected=%v, err=%v", skipped3, rejected3, err3)
	}
}

func TestSecretGuardToMessages_Redact(t *testing.T) {
	body := map[string]any{
		"system": "Admin key: sk-123456789012345678901234",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Check AWS: AKIAIOSFODNN7EXAMPLE",
					},
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type": "base64",
							"data": "somebase64data",
						},
					},
					map[string]any{
						"type":    "tool_result",
						"content": "Secret is PRIVATE_KEY=mysecretkey123456",
					},
				},
			},
		},
	}

	redactedBody, skipped, rejected, _, err := builtin.ApplySecretGuardToMessages(body, map[string]any{"action": "redact"})
	if err != nil || skipped || rejected {
		t.Fatalf("expected redact success, got skipped=%v, rejected=%v, err=%v", skipped, rejected, err)
	}

	sysStr, ok := redactedBody["system"].(string)
	if !ok || strings.Contains(sysStr, "sk-123456789012345678901234") || !strings.Contains(sysStr, "[REDACTED_SECRET:") {
		t.Errorf("system not properly redacted: %q", sysStr)
	}

	msgs, _ := redactedBody["messages"].([]any)
	firstMsg, _ := msgs[0].(map[string]any)
	blocks, _ := firstMsg["content"].([]any)

	textBlock, _ := blocks[0].(map[string]any)
	if strings.Contains(textBlock["text"].(string), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(textBlock["text"].(string), "[REDACTED_SECRET:") {
		t.Errorf("text block not redacted: %v", textBlock)
	}

	trBlock, _ := blocks[2].(map[string]any)
	if strings.Contains(trBlock["content"].(string), "mysecretkey123456") || !strings.Contains(trBlock["content"].(string), "[REDACTED_SECRET:") {
		t.Errorf("tool_result block not redacted: %v", trBlock)
	}
}

func TestSecretGuardToMessages_CleanPass(t *testing.T) {
	body := map[string]any{
		"system": "You are helpful.",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Hello world!",
					},
				},
			},
		},
	}

	_, skipped, rejected, _, err := builtin.ApplySecretGuardToMessages(body, map[string]any{"action": "block"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skipped {
		t.Errorf("expected skipped on clean body")
	}
	if rejected {
		t.Errorf("expected not rejected on clean body")
	}
}

