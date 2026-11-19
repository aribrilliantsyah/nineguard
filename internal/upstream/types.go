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
	Unit string `json:"unit,omitempty"`
	UsedPercent *float64 `json:"used_percent,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	Scope string `json:"scope,omitempty"`
	WindowMinutes int64 `json:"window_minutes,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	ObservationState string `json:"observation_state,omitempty"`
	LimitReached bool `json:"limit_reached,omitempty"`
	Incomplete bool `json:"incomplete,omitempty"`
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
	Source string `json:"source,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	ObservationState string `json:"observation_state,omitempty"`
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
	Source string `json:"source,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	ObservationState string `json:"observation_state,omitempty"`
	// ConsoleURL points admins at the provider's own usage console when NineGuard has no
	// quota telemetry for it (Anthropic API keys expose no limits endpoint).
	ConsoleURL string `json:"console_url,omitempty"`
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
	Source string `json:"source,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	ObservationState string `json:"observation_state,omitempty"`
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
// Inactive rows (model removed, disabled, or no longer allowed) are not enforced.
type ModelQuotaSummary struct {
	ModelID        string     `json:"model_id"`
	Limit          int64      `json:"limit"`
	Used           int64      `json:"used"`
	Remaining      int64      `json:"remaining"`
	Period         string     `json:"period"`
	ResetAt        *time.Time `json:"reset_at,omitempty"`
	Warning        bool       `json:"warning"`
	Inactive       bool       `json:"inactive,omitempty"`
	InactiveReason string     `json:"inactive_reason,omitempty"`
}

// UnifiedQuotaResponse is the response payload for GET /v1/quota and the chat command.
type UnifiedQuotaResponse struct {
	// KeyName is the calling key's display name, so self-service clients (the portal) can label it.
	KeyName       string              `json:"key_name,omitempty"`
	KeyQuota      *ClientKeyBudget    `json:"key_quota,omitempty"`
	ModelQuotas   []ModelQuotaSummary `json:"model_quotas,omitempty"`
	UpstreamPools []MergedPoolQuota   `json:"upstream_pools"`
	LastUpdated   *time.Time          `json:"last_updated,omitempty"`
	// SyncInterval is the background auto-sync interval (e.g. "10m"), shown in the chat command footer.
	SyncInterval string `json:"sync_interval,omitempty"`
}
