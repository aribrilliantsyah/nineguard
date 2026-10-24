package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"nineguard/internal/traffic"
)

type cachedUsage struct {
	summary   *traffic.UsageSummary
	err       error
	expiresAt time.Time
}

var (
	usageCacheMu sync.RWMutex
	usageCache   = make(map[string]cachedUsage) // key: "providerID:range"
)

// FetchUpstreamUsage calls GET /v1/usage on a parent NineGuard instance with 5s timeout and 5m cache (ADR 0013).
func (m *Manager) FetchUpstreamUsage(ctx context.Context, providerID, baseURL, apiKey, rng string) (*traffic.UsageSummary, error) {
	providerID = strings.TrimSpace(providerID)
	rng = strings.ToLower(strings.TrimSpace(rng))
	if rng == "" {
		rng = "30d"
	}
	cacheKey := fmt.Sprintf("%s:%s", providerID, rng)

	// 1. Check cache
	usageCacheMu.RLock()
	c, found := usageCache[cacheKey]
	usageCacheMu.RUnlock()

	if found && time.Now().Before(c.expiresAt) {
		return c.summary, c.err
	}

	// 2. Fetch with 5s timeout
	base := extractBaseURL(baseURL)
	if base == "" {
		return nil, fmt.Errorf("empty base URL for provider %s", providerID)
	}

	reqURL := fmt.Sprintf("%s/v1/usage?range=%s", base, rng)
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if m.instanceID != "" {
		req.Header.Set("X-NineGuard-Trace", m.instanceID)
		req.Header.Set("X-NineGuard-Hop", "1")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	expiresAt := time.Now().Add(5 * time.Minute)

	if err != nil {
		wrappedErr := fmt.Errorf("upstream gateway request failed: %w", err)
		usageCacheMu.Lock()
		usageCache[cacheKey] = cachedUsage{summary: nil, err: wrappedErr, expiresAt: expiresAt}
		usageCacheMu.Unlock()
		return nil, wrappedErr
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		wrappedErr := fmt.Errorf("upstream NineGuard does not support /v1/usage (HTTP 404)")
		usageCacheMu.Lock()
		usageCache[cacheKey] = cachedUsage{summary: nil, err: wrappedErr, expiresAt: expiresAt}
		usageCacheMu.Unlock()
		return nil, wrappedErr
	}

	if resp.StatusCode != http.StatusOK {
		wrappedErr := fmt.Errorf("upstream gateway returned HTTP %d", resp.StatusCode)
		usageCacheMu.Lock()
		usageCache[cacheKey] = cachedUsage{summary: nil, err: wrappedErr, expiresAt: expiresAt}
		usageCacheMu.Unlock()
		return nil, wrappedErr
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to read upstream body: %w", err)
		usageCacheMu.Lock()
		usageCache[cacheKey] = cachedUsage{summary: nil, err: wrappedErr, expiresAt: expiresAt}
		usageCacheMu.Unlock()
		return nil, wrappedErr
	}

	var sum traffic.UsageSummary
	if err := json.Unmarshal(data, &sum); err != nil {
		wrappedErr := fmt.Errorf("failed to parse upstream usage summary JSON: %w", err)
		usageCacheMu.Lock()
		usageCache[cacheKey] = cachedUsage{summary: nil, err: wrappedErr, expiresAt: expiresAt}
		usageCacheMu.Unlock()
		return nil, wrappedErr
	}

	usageCacheMu.Lock()
	usageCache[cacheKey] = cachedUsage{summary: &sum, err: nil, expiresAt: expiresAt}
	usageCacheMu.Unlock()

	return &sum, nil
}
