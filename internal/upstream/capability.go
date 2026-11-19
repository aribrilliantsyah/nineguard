package upstream

import (
	"context"
	"nineguard/internal/providers"
	"time"
)

// SDKQuotaSource supplies read-only observations from the embedded engine, never a remote gateway.
type SDKQuotaSource interface {
	SDKQuotas(context.Context, providers.Provider, time.Time) (*ProviderQuotaResult, error)
}

// AnthropicConsoleUsageURL is where Anthropic API key holders read usage and rate limits.
const AnthropicConsoleUsageURL = "https://platform.claude.com/settings/usage"

func EffectiveQuotaType(p providers.Provider) string {
	if p.Backend == "cliproxy" {
		return "sdk"
	}
	// Anthropic API keys expose no quota endpoint; a 9router/NineGuard tracker there only fails.
	if p.QuotaType == "" || providers.IsAnthropicAPI(p.Route) {
		return "disabled"
	}
	return p.QuotaType
}
func SupportsQuotaTracking(p providers.Provider) bool {
	kind := EffectiveQuotaType(p)
	return kind != "" && kind != "disabled"
}
func (m *Manager) SetSDKQuotaSource(source SDKQuotaSource) { m.sdk = source }
