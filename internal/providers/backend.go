package providers

import (
	"fmt"
	"net/http"
	"strings"
)

// normalizeBackend keeps transport identity independent of the user-chosen prefix and icon.
func normalizeBackend(backend, family, route, apiKey string) (string, string, error) {
	backend = strings.ToLower(strings.TrimSpace(backend))
	family = strings.ToLower(strings.TrimSpace(family))
	if backend == "" {
		backend = "http"
	}
	switch backend {
	case "http":
		if family != "" {
			return "", "", fmt.Errorf("HTTP providers do not have an OAuth family")
		}
		if route == "" {
			return "", "", fmt.Errorf("route endpoint URL is required")
		}
	case "cliproxy":
		switch family {
		case "claude", "codex", "antigravity", "kimi", "xai", "meta", "devin":
		default:
			return "", "", fmt.Errorf("unsupported OAuth provider family")
		}
		if route != "" || apiKey != "" {
			return "", "", fmt.Errorf("OAuth providers use connected accounts, not a custom route or API key")
		}
	default:
		return "", "", fmt.Errorf("unsupported provider backend")
	}
	return backend, family, nil
}

// setUpstreamAuth mirrors the proxy's outbound auth on model probes and syncs: the key goes in
// both Authorization: Bearer (OpenAI-compatible) and x-api-key (Anthropic accepts only this).
// Anthropic's API also requires anthropic-version on every call, including GET /v1/models;
// other upstreams ignore it, so it is only sent to api.anthropic.com.
func setUpstreamAuth(req *http.Request, route, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("x-api-key", apiKey)
	}
	if IsAnthropicAPI(route) {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

// IsAnthropicAPI reports whether route points at Anthropic's first-party API.
func IsAnthropicAPI(route string) bool {
	return strings.Contains(strings.ToLower(route), "api.anthropic.com")
}
