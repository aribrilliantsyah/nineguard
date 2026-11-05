package builtin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type biliHealthEntry struct {
	healthy   bool
	err       error
	checkedAt time.Time
}

var (
	biliHealthMu    sync.RWMutex
	biliHealthCache = make(map[string]biliHealthEntry)
	biliHealthTTL   = 5 * time.Second
)

// ResetBillionContextHealthCache clears the in-memory health probe cache (for tests).
func ResetBillionContextHealthCache() {
	biliHealthMu.Lock()
	biliHealthCache = make(map[string]biliHealthEntry)
	biliHealthMu.Unlock()
}

// WrapBillionContextURL wraps targetURL with biliBase prefix, avoiding double-wrapping.
// Returns biliBase + "/bili/" + targetURL.
func WrapBillionContextURL(biliBase, targetURL string) string {
	biliBase = strings.TrimRight(strings.TrimSpace(biliBase), "/")
	targetURL = strings.TrimSpace(targetURL)
	if biliBase == "" {
		return targetURL
	}
	prefix := biliBase + "/bili/"
	if strings.HasPrefix(targetURL, prefix) {
		return targetURL
	}
	if strings.HasPrefix(targetURL, "/bili/") {
		return biliBase + targetURL
	}
	return prefix + targetURL
}

// ApplyBillionContext validates backend, parses config, and health checks billion-context.
func ApplyBillionContext(
	ctx context.Context,
	client *http.Client,
	settings map[string]any,
	providerBackend string,
) (proxyURL string, skipped bool, skipReason string, err error) {
	if providerBackend == "cliproxy" {
		return "", true, "unsupported_backend", nil
	}

	biliURL := "http://127.0.0.1:8788"
	if settings != nil {
		if u, ok := settings["url"].(string); ok && strings.TrimSpace(u) != "" {
			trimmed := strings.TrimRight(strings.TrimSpace(u), "/")
			parsed, pErr := url.Parse(trimmed)
			if pErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return "", false, "", fmt.Errorf("invalid billion-context url: %s", u)
			}
			biliURL = trimmed
		}
	}

	now := time.Now()
	biliHealthMu.RLock()
	cached, found := biliHealthCache[biliURL]
	biliHealthMu.RUnlock()

	if found && now.Sub(cached.checkedAt) < biliHealthTTL {
		if !cached.healthy {
			return "", false, "", cached.err
		}
		return biliURL, false, "", nil
	}

	probeURL := biliURL + "/__bili/health"
	req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if rErr != nil {
		return "", false, "", fmt.Errorf("billion-context health request error: %w", rErr)
	}

	httpClient := client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}

	resp, dErr := httpClient.Do(req)
	if dErr != nil {
		probeErr := fmt.Errorf("billion-context health probe dial error: %w", dErr)
		biliHealthMu.Lock()
		biliHealthCache[biliURL] = biliHealthEntry{
			healthy:   false,
			err:       probeErr,
			checkedAt: now,
		}
		biliHealthMu.Unlock()
		return "", false, "", probeErr
	}
	defer resp.Body.Close()

	// Treat HTTP 200-299 AND HTTP 403 as healthy
	if (resp.StatusCode >= 200 && resp.StatusCode < 300) || resp.StatusCode == http.StatusForbidden {
		biliHealthMu.Lock()
		biliHealthCache[biliURL] = biliHealthEntry{
			healthy:   true,
			err:       nil,
			checkedAt: now,
		}
		biliHealthMu.Unlock()
		return biliURL, false, "", nil
	}

	statusErr := fmt.Errorf("billion-context unhealthy: HTTP %d", resp.StatusCode)
	biliHealthMu.Lock()
	biliHealthCache[biliURL] = biliHealthEntry{
		healthy:   false,
		err:       statusErr,
		checkedAt: now,
	}
	biliHealthMu.Unlock()
	return "", false, "", statusErr
}
