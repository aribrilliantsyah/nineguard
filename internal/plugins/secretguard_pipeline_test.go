package plugins_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"nineguard/internal/plugins"
)

func TestSecretGuardInPipeline(t *testing.T) {
	pe := plugins.NewPipelineExecutor(nil)

	plg := plugins.Plugin{
		ID:            "secretguard",
		Kind:          plugins.KindBuiltin,
		Name:          "SecretGuard",
		Category:      plugins.CategoryOther,
		Bypassable:    false,
		PipelineOrder: 5,
		FailurePolicy: plugins.PolicyClosed,
	}

	bindingOn := plugins.Binding{
		PluginID:  "secretguard",
		ScopeType: plugins.ScopeGlobal,
		State:     plugins.StateOn,
		Settings:  `{"action":"block"}`,
	}

	// 1. Prompt with API key should be rejected
	leakBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Key: sk-123456789012345678901234567890"}]}`)
	res, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingOn}, nil, "key1", "dev", "gpt-4o", "", false, "", leakBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Rejected {
		t.Fatalf("expected pipeline to reject leaking request")
	}
	if res.RejectCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", res.RejectCode)
	}

	// 2. Change binding to redact
	bindingRedact := plugins.Binding{
		PluginID:  "secretguard",
		ScopeType: plugins.ScopeGlobal,
		State:     plugins.StateOn,
		Settings:  `{"action":"redact"}`,
	}
	res2, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingRedact}, nil, "key1", "dev", "gpt-4o", "", false, "", leakBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Rejected {
		t.Fatalf("expected request not rejected when redact is configured")
	}
	var outMap map[string]any
	_ = json.Unmarshal(res2.Body, &outMap)
	msgs, ok := outMap["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages array")
	}
	firstMsg := msgs[0].(map[string]any)
	content := firstMsg["content"].(string)
	if strings.Contains(content, "sk-123456789012345678901234567890") {
		t.Errorf("expected secret to be redacted from body, got: %s", content)
	}
	if !strings.Contains(content, "[REDACTED_SECRET:API_KEY]") {
		t.Errorf("expected redaction marker, got: %s", content)
	}
}
