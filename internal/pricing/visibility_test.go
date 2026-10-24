package pricing

import (
	"testing"
)

func strPtr(s string) *string {
	return &s
}

func TestShowCost(t *testing.T) {
	// 1. Explicit key override "show"
	if !ShowCost(strPtr("show"), false) {
		t.Fatalf("expected true when key cost_visibility is 'show'")
	}

	// 2. Explicit key override "hide"
	if ShowCost(strPtr("hide"), true) {
		t.Fatalf("expected false when key cost_visibility is 'hide'")
	}

	// 3. Key cost_visibility is nil -> follows global setting
	if !ShowCost(nil, true) {
		t.Fatalf("expected true when key is nil and global is true")
	}
	if ShowCost(nil, false) {
		t.Fatalf("expected false when key is nil and global is false")
	}

	// 4. Key cost_visibility is empty string -> follows global setting
	if !ShowCost(strPtr(""), true) {
		t.Fatalf("expected true when key is empty and global is true")
	}
	if ShowCost(strPtr(""), false) {
		t.Fatalf("expected false when key is empty and global is false")
	}
}
