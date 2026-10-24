package proxy_test

import (
	"encoding/json"
	"testing"

	"nineguard/internal/proxy"
)

func TestInjectStreamUsage(t *testing.T) {
	t.Run("absent stream_options", func(t *testing.T) {
		input := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}],"custom_field":{"foo":"bar"}}`
		out, injected, err := proxy.InjectStreamUsage([]byte(input))
		if err != nil {
			t.Fatalf("InjectStreamUsage failed: %v", err)
		}
		if !injected {
			t.Fatalf("expected injected=true, got false")
		}

		var m map[string]interface{}
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}
		opts, ok := m["stream_options"].(map[string]interface{})
		if !ok {
			t.Fatalf("stream_options not a map: %v", m["stream_options"])
		}
		if opts["include_usage"] != true {
			t.Errorf("include_usage = %v; expected true", opts["include_usage"])
		}
		// Custom field preserved
		custom, ok := m["custom_field"].(map[string]interface{})
		if !ok || custom["foo"] != "bar" {
			t.Errorf("custom_field corrupted: %v", m["custom_field"])
		}
	})

	t.Run("stream_options with include_usage false", func(t *testing.T) {
		input := `{"model":"gpt-4o","stream":true,"stream_options":{"include_usage":false,"other_opt":"x"}}`
		out, injected, err := proxy.InjectStreamUsage([]byte(input))
		if err != nil {
			t.Fatalf("InjectStreamUsage failed: %v", err)
		}
		if !injected {
			t.Fatalf("expected injected=true, got false")
		}

		var m map[string]interface{}
		_ = json.Unmarshal(out, &m)
		opts := m["stream_options"].(map[string]interface{})
		if opts["include_usage"] != true {
			t.Errorf("include_usage = %v; expected true", opts["include_usage"])
		}
		if opts["other_opt"] != "x" {
			t.Errorf("other_opt corrupted: %v", opts["other_opt"])
		}
	})

	t.Run("stream_options already has include_usage true", func(t *testing.T) {
		input := `{"model":"gpt-4o","stream":true,"stream_options":{"include_usage":true}}`
		out, injected, err := proxy.InjectStreamUsage([]byte(input))
		if err != nil {
			t.Fatalf("InjectStreamUsage failed: %v", err)
		}
		if injected {
			t.Fatalf("expected injected=false when client already set include_usage=true")
		}
		if string(out) != input {
			t.Errorf("expected out == input, got %s", string(out))
		}
	})

	t.Run("invalid json returns original bytes without error", func(t *testing.T) {
		input := `not a json`
		out, injected, err := proxy.InjectStreamUsage([]byte(input))
		if err != nil {
			t.Fatalf("expected nil error on invalid json, got %v", err)
		}
		if injected {
			t.Fatalf("expected injected=false for invalid json")
		}
		if string(out) != input {
			t.Errorf("expected original bytes back")
		}
	})
}
