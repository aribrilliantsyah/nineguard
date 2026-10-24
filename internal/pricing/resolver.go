package pricing

import (
	"strings"
	"sync"

	"nineguard/internal/models"
)

type Resolver struct {
	mu        sync.RWMutex
	prices    map[string]Price // keyed by models.NormalizeFamily
	overrides map[string]Price // keyed by exact model ID (lowercased)
}

func NewResolver(prices map[string]Price, overrides map[string]Price) *Resolver {
	r := &Resolver{}
	r.Reload(prices, overrides)
	return r
}

func (r *Resolver) Reload(prices map[string]Price, overrides map[string]Price) {
	normPrices := make(map[string]Price, len(prices))
	for k, v := range prices {
		normPrices[models.NormalizeFamily(k)] = v
	}

	normOverrides := make(map[string]Price, len(overrides)*2)
	for k, v := range overrides {
		normOverrides[strings.ToLower(strings.TrimSpace(k))] = v
		if fam := models.NormalizeFamily(k); fam != "" {
			normOverrides[fam] = v
		}
	}

	r.mu.Lock()
	r.prices = normPrices
	r.overrides = normOverrides
	r.mu.Unlock()
}

func (r *Resolver) PriceFor(modelID string) (*Price, string, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, "", false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. Exact model ID override match
	if p, ok := r.overrides[strings.ToLower(modelID)]; ok {
		cp := p
		return &cp, "override", true
	}

	// 2. Normalized family match on list prices
	fam := models.NormalizeFamily(modelID)
	if p, ok := r.prices[fam]; ok {
		cp := p
		return &cp, "list", true
	}

	// 3. Fallback: normalized family match on overrides (for custom alias prefixes)
	if p, ok := r.overrides[fam]; ok {
		cp := p
		return &cp, "override", true
	}

	return nil, "", false
}

// CalculateCost computes the cost for the resolved model ID if priced.
// Returns nil if no price is found.
func (r *Resolver) CalculateCost(modelID string, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int) *float64 {
	price, _, found := r.PriceFor(modelID)
	if !found || price == nil {
		return nil
	}
	cost := CalculateCostUSD(price, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens)
	return &cost
}
