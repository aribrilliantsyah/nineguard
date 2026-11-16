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

// setAnthropicVersion adds the header Anthropic's API requires on every call, including
// GET /v1/models. Other upstreams ignore it, so it is only sent to api.anthropic.com.
func setAnthropicVersion(req *http.Request, route string) {
	if strings.Contains(strings.ToLower(route), "api.anthropic.com") {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}
