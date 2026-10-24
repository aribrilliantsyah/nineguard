package pricing

import (
	"testing"
)

func TestParseLiteLLM(t *testing.T) {
	fixture := `{
		"sample_spec": {
			"input_cost_per_token": 0.001,
			"output_cost_per_token": 0.002,
			"mode": "chat"
		},
		"text-embedding-3-small": {
			"input_cost_per_token": 0.00000002,
			"output_cost_per_token": 0,
			"mode": "embedding"
		},
		"gpt-4o-incomplete": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0,
			"mode": "chat"
		},
		"openrouter/anthropic/claude-3.5-sonnet": {
			"input_cost_per_token": 0.0000035,
			"output_cost_per_token": 0.0000155,
			"litellm_provider": "openrouter",
			"mode": "chat"
		},
		"claude-3-5-sonnet-20241022": {
			"input_cost_per_token": 0.000003,
			"output_cost_per_token": 0.000015,
			"cache_read_input_token_cost": 0.0000003,
			"cache_creation_input_token_cost": 0.00000375,
			"litellm_provider": "anthropic",
			"mode": "chat"
		},
		"gpt-4o": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000010,
			"litellm_provider": "openai",
			"mode": "chat"
		},
		"responses/custom-response-model": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000002,
			"mode": "responses"
		}
	}`

	prices, err := ParseLiteLLM([]byte(fixture))
	if err != nil {
		t.Fatalf("unexpected error parsing LiteLLM json: %v", err)
	}

	// 1. sample_spec must be skipped
	if _, ok := prices["sample_spec"]; ok {
		t.Fatalf("expected sample_spec to be skipped")
	}

	// 2. embedding mode must be skipped
	if _, ok := prices["text-embedding-3-small"]; ok {
		t.Fatalf("expected non-chat/responses mode to be skipped")
	}

	// 3. zero output cost must be skipped
	if _, ok := prices["gpt-4o-incomplete"]; ok {
		t.Fatalf("expected zero output cost to be skipped")
	}

	// 4. Provider preference: "anthropic" provider preferred over "openrouter" provider for "claude-3-5-sonnet"
	sonnet, ok := prices["claude-3-5-sonnet"]
	if !ok {
		t.Fatalf("expected claude-3-5-sonnet in parsed prices")
	}
	if !floatEquals(sonnet.InputPerMTok, 3.0) {
		t.Fatalf("expected anthropic provider preferred (input 3.0), got %v", sonnet.InputPerMTok)
	}
	if !floatEquals(sonnet.OutputPerMTok, 15.0) {
		t.Fatalf("expected output 15.0, got %v", sonnet.OutputPerMTok)
	}
	if sonnet.CacheReadPerMTok == nil || !floatEquals(*sonnet.CacheReadPerMTok, 0.30) {
		t.Fatalf("expected cache read 0.30, got %v", sonnet.CacheReadPerMTok)
	}
	if sonnet.CacheWritePerMTok == nil || !floatEquals(*sonnet.CacheWritePerMTok, 3.75) {
		t.Fatalf("expected cache write 3.75, got %v", sonnet.CacheWritePerMTok)
	}

	// 5. gpt-4o rates converted to per-1M
	g4o, ok := prices["gpt-4o"]
	if !ok {
		t.Fatalf("expected gpt-4o in parsed prices")
	}
	if !floatEquals(g4o.InputPerMTok, 2.5) || !floatEquals(g4o.OutputPerMTok, 10.0) {
		t.Fatalf("expected gpt-4o input 2.5, output 10.0, got %v, %v", g4o.InputPerMTok, g4o.OutputPerMTok)
	}

	// 6. responses mode accepted
	respModel, ok := prices["custom-response-model"]
	if !ok {
		t.Fatalf("expected custom-response-model in parsed prices")
	}
	if !floatEquals(respModel.InputPerMTok, 1.0) || !floatEquals(respModel.OutputPerMTok, 2.0) {
		t.Fatalf("expected custom-response-model input 1.0, output 2.0, got %v, %v", respModel.InputPerMTok, respModel.OutputPerMTok)
	}
}

func TestLoadSnapshot(t *testing.T) {
	snapshot, err := LoadSnapshot()
	if err != nil {
		t.Fatalf("unexpected error loading embedded snapshot: %v", err)
	}
	if len(snapshot) == 0 {
		t.Fatalf("expected non-empty embedded snapshot")
	}

	// Verify key models exist in snapshot
	expectedModels := []string{"claude-3-5-sonnet", "gpt-4o", "gpt-4o-mini"}
	for _, m := range expectedModels {
		if _, ok := snapshot[m]; !ok {
			t.Errorf("expected model %q in embedded snapshot", m)
		}
	}
}
