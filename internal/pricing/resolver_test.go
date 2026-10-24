package pricing

import (
	"testing"
)

func TestResolver_PriceFor(t *testing.T) {
	crSonnet := 0.3
	cwSonnet := 3.75
	listPrices := map[string]Price{
		"claude-3-5-sonnet": {
			InputPerMTok:      3.0,
			OutputPerMTok:     15.0,
			CacheReadPerMTok:  &crSonnet,
			CacheWritePerMTok: &cwSonnet,
		},
		"gpt-4o": {
			InputPerMTok:  2.5,
			OutputPerMTok: 10.0,
		},
	}

	overrides := map[string]Price{
		"custom/my-model": {
			InputPerMTok:  1.0,
			OutputPerMTok: 2.0,
		},
		"anthropic/claude-3-5-sonnet-20241022": {
			InputPerMTok:  4.0,
			OutputPerMTok: 20.0,
		},
	}

	resolver := NewResolver(listPrices, overrides)

	// 1. Direct override match
	p, source, found := resolver.PriceFor("custom/my-model")
	if !found || source != "override" || p.InputPerMTok != 1.0 {
		t.Fatalf("expected override price for custom/my-model, got found=%v, source=%v, p=%+v", found, source, p)
	}

	// 2. Override precedence over list price for exact model ID
	p, source, found = resolver.PriceFor("anthropic/claude-3-5-sonnet-20241022")
	if !found || source != "override" || p.InputPerMTok != 4.0 {
		t.Fatalf("expected override to take precedence over list, got found=%v, source=%v, p=%+v", found, source, p)
	}

	// 3. Normalized family match on list price
	// "9router/anthropic/claude-3.5-sonnet" normalizes to "claude-3-5-sonnet"
	p, source, found = resolver.PriceFor("9router/anthropic/claude-3.5-sonnet")
	if !found || source != "list" || p.InputPerMTok != 3.0 {
		t.Fatalf("expected list price for normalized family, got found=%v, source=%v, p=%+v", found, source, p)
	}

	// 4. Case-insensitivity check
	p, source, found = resolver.PriceFor("GPT-4O-2024-08-06")
	if !found || source != "list" || p.InputPerMTok != 2.5 {
		t.Fatalf("expected case-insensitive normalized family match for gpt-4o, got found=%v, source=%v, p=%+v", found, source, p)
	}

	// 5. Unknown model
	p, source, found = resolver.PriceFor("unknown/phantom-model-xyz")
	if found || source != "" || p != nil {
		t.Fatalf("expected unknown model not found, got found=%v, source=%v, p=%+v", found, source, p)
	}
}

func TestResolver_Reload(t *testing.T) {
	resolver := NewResolver(nil, nil)

	_, _, found := resolver.PriceFor("gpt-4o")
	if found {
		t.Fatalf("expected not found on empty resolver")
	}

	resolver.Reload(map[string]Price{
		"gpt-4o": {InputPerMTok: 5.0, OutputPerMTok: 15.0},
	}, nil)

	p, source, found := resolver.PriceFor("gpt-4o")
	if !found || source != "list" || p.InputPerMTok != 5.0 {
		t.Fatalf("expected price found after reload, got found=%v, source=%v, p=%+v", found, source, p)
	}
}
