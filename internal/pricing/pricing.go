package pricing

import "time"

// Price represents cost rates per 1 million tokens (USD).
type Price struct {
	InputPerMTok      float64  `json:"input_per_mtok"`
	OutputPerMTok     float64  `json:"output_per_mtok"`
	CacheReadPerMTok  *float64 `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok *float64 `json:"cache_write_per_mtok,omitempty"`
}

// ModelPriceRow represents a model family price in model_prices.
type ModelPriceRow struct {
	Family            string    `json:"family"`
	InputPerMTok      float64   `json:"input_per_mtok"`
	OutputPerMTok     float64   `json:"output_per_mtok"`
	CacheReadPerMTok  *float64  `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok *float64  `json:"cache_write_per_mtok,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// OverrideRow represents a model ID override in model_price_overrides.
type OverrideRow struct {
	ModelID           string    `json:"model_id"`
	InputPerMTok      float64   `json:"input_per_mtok"`
	OutputPerMTok     float64   `json:"output_per_mtok"`
	CacheReadPerMTok  *float64  `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok *float64  `json:"cache_write_per_mtok,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ModelWithPrice combines a registered model with its resolved pricing info.
type ModelWithPrice struct {
	ModelID           string   `json:"model_id"`
	Family            string   `json:"family"`
	Source            string   `json:"source"` // "override", "list", "none"
	InputPerMTok      *float64 `json:"input_per_mtok,omitempty"`
	OutputPerMTok     *float64 `json:"output_per_mtok,omitempty"`
	CacheReadPerMTok  *float64 `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok *float64 `json:"cache_write_per_mtok,omitempty"`
	IsOverride        bool     `json:"is_override"`
}

// CalculateCostUSD calculates the USD cost according to Spec §5.2:
//
//	uncached = max(0, prompt - cache_read - cache_write)
//	cost = uncached     * input
//	     + cache_read   * (cache_read_rate  ?? input)
//	     + cache_write  * (cache_write_rate ?? input)
//	     + completion   * output                       (all rates per token = per_mtok / 1e6)
func CalculateCostUSD(price *Price, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int) float64 {
	if price == nil {
		return 0
	}
	uncached := promptTokens - cacheReadTokens - cacheWriteTokens
	if uncached < 0 {
		uncached = 0
	}

	inputRate := price.InputPerMTok
	outputRate := price.OutputPerMTok

	cacheReadRate := inputRate
	if price.CacheReadPerMTok != nil {
		cacheReadRate = *price.CacheReadPerMTok
	}

	cacheWriteRate := inputRate
	if price.CacheWritePerMTok != nil {
		cacheWriteRate = *price.CacheWritePerMTok
	}

	totalPerMTok := float64(uncached)*inputRate +
		float64(cacheReadTokens)*cacheReadRate +
		float64(cacheWriteTokens)*cacheWriteRate +
		float64(completionTokens)*outputRate

	return totalPerMTok / 1e6
}
