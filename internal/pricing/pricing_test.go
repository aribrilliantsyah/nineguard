package pricing

import (
	"math"
	"testing"
)

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCalculateCostUSD_Standard(t *testing.T) {
	price := &Price{
		InputPerMTok:  3.0,
		OutputPerMTok: 15.0,
	}

	cost := CalculateCostUSD(price, 1000, 500, 0, 0)
	expected := (1000.0*3.0 + 500.0*15.0) / 1e6
	if !floatEquals(cost, expected) {
		t.Fatalf("expected cost %v, got %v", expected, cost)
	}
}

func TestCalculateCostUSD_WithCacheRates(t *testing.T) {
	cacheReadRate := 0.30
	cacheWriteRate := 3.75
	price := &Price{
		InputPerMTok:      3.0,
		OutputPerMTok:     15.0,
		CacheReadPerMTok:  &cacheReadRate,
		CacheWritePerMTok: &cacheWriteRate,
	}

	// prompt = 10000, read = 8000, write = 1000 -> uncached = 1000
	cost := CalculateCostUSD(price, 10000, 1000, 8000, 1000)
	expected := (1000.0*3.0 + 8000.0*0.30 + 1000.0*3.75 + 1000.0*15.0) / 1e6
	if !floatEquals(cost, expected) {
		t.Fatalf("expected cost %v, got %v", expected, cost)
	}
}

func TestCalculateCostUSD_CacheFallbackToInput(t *testing.T) {
	price := &Price{
		InputPerMTok:  3.0,
		OutputPerMTok: 15.0,
		// CacheReadPerMTok and CacheWritePerMTok are nil
	}

	// When cache rates are nil, they should fall back to InputPerMTok
	cost := CalculateCostUSD(price, 5000, 500, 2000, 1000)
	expected := (5000.0*3.0 + 500.0*15.0) / 1e6
	if !floatEquals(cost, expected) {
		t.Fatalf("expected cost %v, got %v", expected, cost)
	}
}

func TestCalculateCostUSD_CacheExceedsPromptClamped(t *testing.T) {
	cacheReadRate := 0.5
	cacheWriteRate := 1.0
	price := &Price{
		InputPerMTok:      3.0,
		OutputPerMTok:     15.0,
		CacheReadPerMTok:  &cacheReadRate,
		CacheWritePerMTok: &cacheWriteRate,
	}

	// prompt = 1000, read = 800, write = 500 -> uncached clamped to 0
	cost := CalculateCostUSD(price, 1000, 100, 800, 500)
	expected := (0.0*3.0 + 800.0*0.5 + 500.0*1.0 + 100.0*15.0) / 1e6
	if !floatEquals(cost, expected) {
		t.Fatalf("expected cost %v, got %v", expected, cost)
	}
}

func TestCalculateCostUSD_NilPrice(t *testing.T) {
	cost := CalculateCostUSD(nil, 1000, 1000, 0, 0)
	if cost != 0 {
		t.Fatalf("expected 0 cost for nil price, got %v", cost)
	}
}
