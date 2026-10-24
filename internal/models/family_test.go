package models_test

import (
	"testing"

	"nineguard/internal/models"
)

func TestNormalizeFamily(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Simple IDs
		{"gpt-4o", "gpt-4o"},
		{"claude-3-5-sonnet", "claude-3-5-sonnet"},

		// Dots and underscores normalized to hyphens
		{"claude-opus-4.5", "claude-opus-4-5"},
		{"claude_opus_4_5", "claude-opus-4-5"},

		// Date suffixes (YYYYMMDD and YYYY-MM-DD)
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"gpt-4o-2024-08-06", "gpt-4o"},
		{"claude-3-opus-20240229", "claude-3-opus"},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},

		// Provider prefixes (keep only after last '/')
		{"9router/claude-opus-4.5", "claude-opus-4-5"},
		{"nineguard/9router/claude-opus-4.5", "claude-opus-4-5"},
		{"openrouter/anthropic/claude-opus-4.5", "claude-opus-4-5"},

		// Cloud/region prefixes (strip repeatable leading prefixes)
		{"anthropic.claude-opus-4-6-v1", "claude-opus-4-6"},
		{"us.anthropic.claude-3-5-sonnet", "claude-3-5-sonnet"},
		{"global.anthropic.claude-opus-4-7", "claude-opus-4-7"},
		{"eu.anthropic.claude-3-haiku", "claude-3-haiku"},
		{"apac.anthropic.claude-sonnet", "claude-sonnet"},
		{"au.anthropic.claude-opus", "claude-opus"},

		// Version & tag suffixes
		{"claude-3-5-sonnet-latest", "claude-3-5-sonnet"},
		{"claude-3.7-sonnet:thinking", "claude-3-7-sonnet"},
		{"claude-3.7-sonnet-thinking", "claude-3-7-sonnet"},
		{"gemini-2.0-flash:free", "gemini-2-0-flash"},
		{"gemini-2.0-flash:beta", "gemini-2-0-flash"},
		{"anthropic.claude-opus-4-6-v1:0", "claude-opus-4-6"},
		{"model@20250929", "model"},
		{"gemini-3.8-flash-high", "gemini-3-8-flash"},
		{"gemini-3.8-flash-medium", "gemini-3-8-flash"},
		{"gemini-3.8-flash-low", "gemini-3-8-flash"},
		{"gemini-3.8-flash-nano", "gemini-3-8-flash"},
		{"gemini-3.8-flash-nonthinking", "gemini-3-8-flash"},
		{"gemini-3.8-flash-n", "gemini-3-8-flash"},

		// Whitespace & uppercase
		{"  OpenRouter/Anthropic/Claude-3.5-Sonnet  ", "claude-3-5-sonnet"},
	}

	for _, tc := range tests {
		actual := models.NormalizeFamily(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizeFamily(%q) = %q; expected %q", tc.input, actual, tc.expected)
		}

		// Idempotence: f(f(x)) == f(x)
		second := models.NormalizeFamily(actual)
		if second != actual {
			t.Errorf("NormalizeFamily not idempotent for %q: f(%q)=%q, f(f(%q))=%q", tc.input, tc.input, actual, tc.input, second)
		}
	}
}

func TestIsModelFallback(t *testing.T) {
	tests := []struct {
		resolved string
		served   string
		expected bool
	}{
		// Same family -> not fallback
		{"9router/claude-opus-4.5", "claude-opus-4-5-20251101", false},
		{"9router/claude-opus-4.5", "claude-opus-4.5", false},
		{"custom-model", "custom-model", false},
		{"openrouter/openai/gpt-4o", "gpt-4o-2024-08-06", false},

		// Different family -> fallback
		{"9router/claude-opus-4.5", "claude-sonnet-4-5-20250929", true},
		{"9router/claude-opus-4.5", "claude-3-5-haiku-20241022", true},
		{"openai/gpt-4o", "gpt-4o-mini", true},

		// Served model empty or missing -> no evidence, not fallback
		{"9router/claude-opus-4.5", "", false},
		{"", "claude-opus-4.5", false},
	}

	for _, tc := range tests {
		actual := models.IsModelFallback(tc.resolved, tc.served)
		if actual != tc.expected {
			t.Errorf("IsModelFallback(%q, %q) = %v; expected %v", tc.resolved, tc.served, actual, tc.expected)
		}
	}
}
