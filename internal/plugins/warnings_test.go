package plugins

import (
	"strings"
	"testing"
)

func TestComputeWarnings_OutputStyleOverlap(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "caveman", Name: "Caveman", Category: CategoryOutputStyle}, EffectiveState: StateOn},
		{Plugin: Plugin{ID: "ponytail", Name: "Ponytail", Category: CategoryOutputStyle}, EffectiveState: StateOn},
	}

	w := ComputeWarnings(effective, false, "", "")
	if len(w) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(w))
	}
	if w[0].Code != "output_style_overlap" {
		t.Fatalf("expected code output_style_overlap, got %s", w[0].Code)
	}
	if !strings.Contains(w[0].Message, "Caveman") || !strings.Contains(w[0].Message, "Ponytail") {
		t.Errorf("expected warning to mention Caveman and Ponytail, got %s", w[0].Message)
	}
}

func TestComputeWarnings_InputCompressionOverlap(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "headroom", Name: "Headroom", Category: CategoryInputCompression}, EffectiveState: StateOn},
		{Plugin: Plugin{ID: "billion-context", Name: "Billion Context", Category: CategoryInputCompression}, EffectiveState: StateOn},
	}

	w := ComputeWarnings(effective, false, "", "")
	if len(w) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(w))
	}
	if w[0].Code != "input_compression_overlap" {
		t.Fatalf("expected code input_compression_overlap, got %s", w[0].Code)
	}
	expectedMsg := "Headroom and Billion Context both compress message history. Stacking them causes double compression and degrades context quality. Enable only one."
	if w[0].Message != expectedMsg {
		t.Errorf("expected warning message %q, got %q", expectedMsg, w[0].Message)
	}
}

func TestComputeWarnings_InputAndOutputNoOverlap(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "headroom", Name: "Headroom", Category: CategoryInputCompression}, EffectiveState: StateOn},
		{Plugin: Plugin{ID: "caveman", Name: "Caveman", Category: CategoryOutputStyle}, EffectiveState: StateOn},
	}

	w := ComputeWarnings(effective, false, "", "")
	if len(w) != 0 {
		t.Fatalf("expected 0 warnings for input + output, got %d", len(w))
	}
}

func TestComputeWarnings_UpstreamTokenSaving(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "headroom", Name: "Headroom", Category: CategoryInputCompression}, EffectiveState: StateOn},
	}

	w := ComputeWarnings(effective, true, "RTK enabled", "9router")
	if len(w) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(w))
	}
	if w[0].Code != "upstream_token_saving" {
		t.Fatalf("expected code upstream_token_saving, got %s", w[0].Code)
	}
	if !strings.Contains(w[0].Message, "9router") || !strings.Contains(w[0].Message, "RTK enabled") {
		t.Errorf("expected warning to mention provider and note, got %s", w[0].Message)
	}
}

func TestComputeWarnings_InactivePluginsIgnored(t *testing.T) {
	effective := []ResolvedPlugin{
		{Plugin: Plugin{ID: "caveman", Name: "Caveman", Category: CategoryOutputStyle}, EffectiveState: StateOn},
		{Plugin: Plugin{ID: "ponytail", Name: "Ponytail", Category: CategoryOutputStyle}, EffectiveState: StateOff},
	}

	w := ComputeWarnings(effective, false, "", "")
	if len(w) != 0 {
		t.Fatalf("expected 0 warnings when second plugin is off, got %d", len(w))
	}
}
