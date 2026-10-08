package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyHeadroom_IncrementalAndPreservation(t *testing.T) {
	var receivedPayload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/compress" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]any{
				{"role": "system", "content": "system instruction"},
				{"role": "user", "content": "compressed user prompt"},
			},
			"tokens_saved": 42,
		})
	}))
	defer ts.Close()

	body := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "system", "content": "system instruction"},
			map[string]any{"role": "user", "content": "original user prompt"},
			map[string]any{"role": "assistant", "content": "assistant reply"},
			map[string]any{"role": "user", "content": "second user prompt"},
		},
		"tools": []any{map[string]any{"type": "function"}},
	}

	settings := map[string]any{
		"url":  ts.URL,
		"mode": "incremental",
	}

	res, saved, skipped, _, err := ApplyHeadroom(context.Background(), ts.Client(), body, settings)
	if err != nil || skipped {
		t.Fatalf("headroom failed: %v", err)
	}
	if saved != 42 {
		t.Fatalf("expected 42 tokens saved, got %d", saved)
	}

	// Check frozen_message_count sent to headroom
	cfg, _ := receivedPayload["config"].(map[string]any)
	frozen, _ := cfg["frozen_message_count"].(float64)
	if int(frozen) != 3 { // index after assistant (idx 2) is 3
		t.Fatalf("expected frozen count 3, got %v", frozen)
	}

	// Verify tools preserved
	if res["tools"] == nil {
		t.Fatalf("tools should be preserved")
	}
}

func TestApplyHeadroom_CompressionSkipped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"compression_skipped": true,
			"tokens_saved":        0,
		})
	}))
	defer ts.Close()

	body := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	settings := map[string]any{
		"url": ts.URL,
	}

	res, saved, skipped, _, err := ApplyHeadroom(context.Background(), ts.Client(), body, settings)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if skipped {
		t.Fatalf("should not be marked as skipped plugin when compression_skipped is true")
	}
	if saved != 0 {
		t.Fatalf("expected 0 saved, got %d", saved)
	}
	if res["messages"] == nil {
		t.Fatalf("expected original body returned")
	}
}
