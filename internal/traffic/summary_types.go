package traffic

type TokensBreakdown struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Total      int64 `json:"total"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
}

type ThinkingBreakdown struct {
	Requested int64 `json:"requested"`
	Delivered int64 `json:"delivered"`
	Lost      int64 `json:"lost"`
}

type CostBreakdown struct {
	USD              float64 `json:"usd"`
	Display          float64 `json:"display"`
	DisplayFormatted string  `json:"display_formatted"`
	Currency         string  `json:"currency"`
	Rate             float64 `json:"rate"`
	UnpricedRequests int64   `json:"unpriced_requests"`
}

type ComparisonResult struct {
	Book     string  `json:"book"`
	Multiple float64 `json:"multiple"`
	Text     string  `json:"text"`
}

type DailyUsagePoint struct {
	Date     string   `json:"date"`
	Requests int64    `json:"requests"`
	Tokens   int64    `json:"tokens"`
	Cost     *float64 `json:"cost,omitempty"`
}

type ModelDailyUsage struct {
	Date  string `json:"date"`
	Total int64  `json:"total"`
}

type ModelUsagePoint struct {
	ModelID    string            `json:"model_id"`
	Prompt     int64             `json:"prompt"`
	Completion int64             `json:"completion"`
	Total      int64             `json:"total"`
	Cost       *float64          `json:"cost,omitempty"`
	PctTokens  float64           `json:"pct_tokens"`
	Daily      []ModelDailyUsage `json:"daily"`
}

type UpstreamUsageEntry struct {
	Provider string        `json:"provider"`
	Summary  *UsageSummary `json:"summary,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type UsageSummary struct {
	Range             string               `json:"range"`
	From              string               `json:"from"`
	To                string               `json:"to"`
	Timezone          string               `json:"timezone"`
	Requests          int64                `json:"requests"`
	Errors            int64                `json:"errors"`
	ErrorRatePct      float64              `json:"error_rate_pct"`
	Sessions          int64                `json:"sessions"`
	ActiveDays        int64                `json:"active_days"`
	PeakHour          int                  `json:"peak_hour"`
	PeakHourFormatted string               `json:"peak_hour_formatted"`
	Tokens            TokensBreakdown      `json:"tokens"`
	EstimatedRequests int64                `json:"estimated_requests"`
	CacheHitPct       float64              `json:"cache_hit_pct"`
	Thinking          ThinkingBreakdown    `json:"thinking"`
	Fallbacks         int64                `json:"fallbacks"`
	FavoriteModel     string               `json:"favorite_model"`
	Cost              *CostBreakdown       `json:"cost,omitempty"`
	Daily             []DailyUsagePoint    `json:"daily"`
	Models            []ModelUsagePoint    `json:"models"`
	Comparison        *ComparisonResult    `json:"comparison,omitempty"`
	UpstreamUsage     []UpstreamUsageEntry `json:"upstream_usage,omitempty"`
}
