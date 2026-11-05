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
	res, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingOn}, nil, "key1", "dev", "gpt-4o", "", "http", "chat", false, "", leakBody)
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
	res2, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingRedact}, nil, "key1", "dev", "gpt-4o", "", "http", "chat", false, "", leakBody)
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

	// 3. Body {} without messages: should skip secretguard (not 503!)
	emptyBody := []byte(`{}`)
	res3, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingOn}, nil, "key1", "dev", "gpt-4o", "", "http", "chat", false, "", emptyBody)
	if err != nil {
		t.Fatalf("unexpected error on empty body: %v", err)
	}
	if res3.Rejected {
		t.Fatalf("expected empty body not rejected with 503, got code %d: %s", res3.RejectCode, res3.RejectMessage)
	}
	hasSkipped := false
	for _, sk := range res3.PluginsSkipped {
		if strings.HasPrefix(sk, "secretguard:no_messages") {
			hasSkipped = true
		}
	}
	if !hasSkipped {
		t.Errorf("expected secretguard:no_messages in PluginsSkipped, got %v", res3.PluginsSkipped)
	}

	// 4. Body with messages = "x": should return 400 Invalid request
	invalidMsgsBody := []byte(`{"messages":"x"}`)
	res4, _, err := pe.Execute(context.Background(), []plugins.Plugin{plg}, []plugins.Binding{bindingOn}, nil, "key1", "dev", "gpt-4o", "", "http", "chat", false, "", invalidMsgsBody)
	if err != nil {
		t.Fatalf("unexpected error on invalid messages body: %v", err)
	}
	if !res4.Rejected {
		t.Fatalf("expected rejection for invalid messages format")
	}
	if res4.RejectCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", res4.RejectCode)
	}
	if res4.RejectMessage != "Invalid request: messages must be an array of message objects" {
		t.Errorf("expected rejection message 'Invalid request: messages must be an array of message objects', got %q", res4.RejectMessage)
	}

	// 5. Test other plugins (caveman, ponytail, headroom) for same no_messages skip & invalid format 400
	for _, pid := range []string{"caveman", "ponytail", "headroom"} {
		pItem := plugins.Plugin{
			ID:            pid,
			Kind:          plugins.KindBuiltin,
			FailurePolicy: plugins.PolicyClosed,
		}
		bItem := plugins.Binding{
			PluginID:  pid,
			ScopeType: plugins.ScopeGlobal,
			State:     plugins.StateOn,
		}
		resSkip, _, _ := pe.Execute(context.Background(), []plugins.Plugin{pItem}, []plugins.Binding{bItem}, nil, "k", "dev", "gpt-4o", "", "http", "chat", false, "", emptyBody)
		if resSkip.Rejected {
			t.Errorf("[%s] expected empty body not rejected, got code %d", pid, resSkip.RejectCode)
		}
		resBad, _, _ := pe.Execute(context.Background(), []plugins.Plugin{pItem}, []plugins.Binding{bItem}, nil, "k", "dev", "gpt-4o", "", "http", "chat", false, "", invalidMsgsBody)
		if !resBad.Rejected || resBad.RejectCode != http.StatusBadRequest {
			t.Errorf("[%s] expected 400 Bad Request on invalid messages, got rejected=%v code=%d", pid, resBad.Rejected, resBad.RejectCode)
		}
	}
}
