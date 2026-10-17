package upstream

import "time"

// QuotaBucket represents a specific limit window or credit balance.
type QuotaBucket struct {
	Name                string     `json:"name"`                 // e.g. "5h Session", "Weekly", "Credits"
	Used                int64      `json:"used"`
	Total               int64      `json:"total"`
	Remaining           int64      `json:"remaining"`
	RemainingPercentage int        `json:"remaining_percentage"` // 0 - 100
	ResetAt             *time.Time `json:"reset_at,omitempty"`
	IsCreditBalance     bool       `json:"is_credit_balance"`
	Warning             bool       `json:"warning"`              // true if remaining_percentage < 25
}

// ConnectionQuota represents quota data for a specific upstream account connection.
type ConnectionQuota struct {
	ConnectionID   string        `json:"connection_id"`
	Provider       string        `json:"provider"`      // e.g. "anthropic", "google", "openai"
	AccountLabel   string        `json:"account_label"` // Email or display name
	Plan           string        `json:"plan,omitempty"`
	Buckets        []QuotaBucket `json:"buckets"`
	ResetCredits   int           `json:"reset_credits,omitempty"`
	NextGrantID    string        `json:"next_grant_id,omitempty"`
	ResetExpiresAt *string       `json:"reset_expires_at,omitempty"`
	Status         string        `json:"status"` // "ok", "degraded", "error"
	ErrorMsg       string        `json:"error_msg,omitempty"`
	LastUpdated    time.Time     `json:"last_updated"`
}

// ProviderQuotaResult holds the complete quota state for an upstream provider.
type ProviderQuotaResult struct {
	ProviderID   string            `json:"provider_id"`
	ProviderName string            `json:"provider_name"`
	QuotaType    string            `json:"quota_type"` // "disabled", "9router", "nineguard"
	Status       string            `json:"status"`     // "ok", "degraded", "error", "unsupported"
	ErrorMsg     string            `json:"error_msg,omitempty"`
	Connections  []ConnectionQuota `json:"connections"`
	LastUpdated  time.Time         `json:"last_updated"`
}

// MergedPoolQuota represents client-facing aggregated quotas grouped by provider & family.
type MergedPoolQuota struct {
	Provider      string        `json:"provider"`                 // Provider display name / prefix
	SubProvider   string        `json:"sub_provider,omitempty"`   // Underlying service provider (e.g. "antigravity", "copilot")
	Family        string        `json:"family"`                   // Composite family name (e.g. "9router · Antigravity")
	Buckets       []QuotaBucket `json:"buckets"`
	AllowedModels []string      `json:"allowed_models,omitempty"`
	Status        string        `json:"status"`
	Warning       bool          `json:"warning"`
}

// ClientKeyBudget represents the calling key's own token budget in key mode.
type ClientKeyBudget struct {
	Mode      string     `json:"mode"`   // "key"
	Limit     int64      `json:"limit"`
	Used      int64      `json:"used"`
	Remaining int64      `json:"remaining"`
	Period    string     `json:"period"` // "daily", "weekly", "monthly", "total"
	ResetAt   *time.Time `json:"reset_at,omitempty"`
	Warning   bool       `json:"warning"`
}

// ModelQuotaSummary represents the calling key's per-model token budget in model mode.
type ModelQuotaSummary struct {
	ModelID   string     `json:"model_id"`
	Limit     int64      `json:"limit"`
	Used      int64      `json:"used"`
	Remaining int64      `json:"remaining"`
	Period    string     `json:"period"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
	Warning   bool       `json:"warning"`
}

// UnifiedQuotaResponse is the response payload for GET /v1/quota and the chat command.
type UnifiedQuotaResponse struct {
	KeyQuota      *ClientKeyBudget    `json:"key_quota,omitempty"`
	ModelQuotas   []ModelQuotaSummary `json:"model_quotas,omitempty"`
	UpstreamPools []MergedPoolQuota   `json:"upstream_pools"`
	LastUpdated   *time.Time          `json:"last_updated,omitempty"`
	// SyncInterval is the background auto-sync interval (e.g. "10m"), shown in the chat command footer.
	SyncInterval string `json:"sync_interval,omitempty"`
}
