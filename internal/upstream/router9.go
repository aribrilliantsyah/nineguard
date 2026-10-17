package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"nineguard/internal/providers"
)

// Router9Adapter handles communicating with 9router instance to query provider connections and usage quotas.
type Router9Adapter struct {
	client    *http.Client
	cookieMu  sync.RWMutex
	cookieJar map[string][]*http.Cookie // providerID -> session cookies
}

func NewRouter9Adapter() *Router9Adapter {
	return &Router9Adapter{
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
		cookieJar: make(map[string][]*http.Cookie),
	}
}

// extractBaseURL extracts scheme + host from any route URL.
func extractBaseURL(route string) string {
	u, err := url.Parse(strings.TrimSpace(route))
	if err != nil || u.Host == "" {
		r := strings.TrimRight(strings.TrimSpace(route), "/")
		if strings.HasSuffix(r, "/v1") {
			r = strings.TrimSuffix(r, "/v1")
		} else if strings.HasSuffix(r, "/v1beta") {
			r = strings.TrimSuffix(r, "/v1beta")
		}
		return strings.TrimRight(r, "/")
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

// FetchQuotas queries connections and their respective quotas from 9router.
func (a *Router9Adapter) FetchQuotas(ctx context.Context, p providers.Provider, force bool) (*ProviderQuotaResult, error) {
	baseURL := extractBaseURL(p.Route)
	if baseURL == "" {
		return nil, fmt.Errorf("empty base URL for provider %s", p.ID)
	}

	res := &ProviderQuotaResult{
		ProviderID:   p.ID,
		ProviderName: p.Name,
		QuotaType:    "9router",
		Status:       "ok",
		LastUpdated:  time.Now(),
		Connections:  make([]ConnectionQuota, 0),
	}

	// 1. Fetch connections list
	conns, err := a.fetchConnections(ctx, p, baseURL)
	if err != nil {
		res.Status = "error"
		res.ErrorMsg = fmt.Sprintf("failed to fetch connections: %v", err)
		return res, nil
	}

	if len(conns) == 0 {
		return res, nil
	}

	// 2. Concurrently fetch usage for each connection (bounded concurrency)
	type connResult struct {
		conn ConnectionQuota
		err  error
	}

	resultsChan := make(chan connResult, len(conns))
	semaphore := make(chan struct{}, 3) // max 3 concurrent requests to avoid flood

	for _, c := range conns {
		go func(connRaw router9Connection) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			cq, fetchErr := a.fetchConnectionQuota(ctx, p, baseURL, connRaw, force)
			resultsChan <- connResult{conn: cq, err: fetchErr}
		}(c)
	}

	hasAnyError := false
	for i := 0; i < len(conns); i++ {
		r := <-resultsChan
		res.Connections = append(res.Connections, r.conn)
		if r.err != nil || r.conn.Status != "ok" {
			hasAnyError = true
		}
	}

	if hasAnyError {
		res.Status = "degraded"
	}

	return res, nil
}

type router9Connection struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	IsActive    bool   `json:"isActive"`
}

type router9ClientResponse struct {
	Connections []router9Connection `json:"connections"`
}

func (a *Router9Adapter) fetchConnections(ctx context.Context, p providers.Provider, baseURL string) ([]router9Connection, error) {
	connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	reqURL := baseURL + "/api/providers/client?pageSize=500"
	respBody, err := a.doWithAuth(connCtx, p, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	var data router9ClientResponse
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("invalid json response from 9router: %w", err)
	}

	return data.Connections, nil
}

type router9UsageQuotaItem struct {
	Used                *float64 `json:"used"`
	Total               *float64 `json:"total"`
	Remaining           *float64 `json:"remaining"`
	RemainingPercentage *float64 `json:"remainingPercentage"`
	ResetAt             *string  `json:"resetAt"`
	ResetTime           *string  `json:"resetTime"`
	DisplayName         string   `json:"displayName"`
	Unlimited           bool     `json:"unlimited"`
}

type router9ResetCredits struct {
	AvailableCount int     `json:"availableCount"`
	NextGrantID    *string `json:"nextGrantId"`
	ExpiresAt      *string `json:"expiresAt"`
}

type router9UsageResponse struct {
	Plan          string                           `json:"plan"`
	ResetDate     *string                          `json:"resetDate"`
	Quotas        map[string]router9UsageQuotaItem `json:"quotas"`
	Credits       *float64                         `json:"credits"`
	CreditBalance *float64                         `json:"creditBalance"`
	Balance       *float64                         `json:"balance"`
	ResetCredits  *router9ResetCredits             `json:"resetCredits"`
	Message       string                           `json:"message"`
	Error         string                           `json:"error"`
}

func (a *Router9Adapter) fetchConnectionQuota(ctx context.Context, p providers.Provider, baseURL string, conn router9Connection, force bool) (ConnectionQuota, error) {
	cq := ConnectionQuota{
		ConnectionID: conn.ID,
		Provider:     conn.Provider,
		AccountLabel: conn.DisplayName,
		Status:       "ok",
		LastUpdated:  time.Now(),
		Buckets:      make([]QuotaBucket, 0),
	}
	if cq.AccountLabel == "" {
		cq.AccountLabel = conn.Email
	}
	if cq.AccountLabel == "" {
		cq.AccountLabel = conn.Name
	}
	if cq.AccountLabel == "" {
		cq.AccountLabel = conn.ID
	}

	forceParam := ""
	timeout := 15 * time.Second
	if force {
		forceParam = "?force=1"
		timeout = 35 * time.Second
	}
	reqURL := fmt.Sprintf("%s/api/usage/%s%s", baseURL, url.PathEscape(conn.ID), forceParam)

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var data router9UsageResponse
	var fellBackToCache bool

	respBody, err := a.doWithAuth(callCtx, p, "GET", reqURL, nil)
	if err != nil && force {
		// Live upstream refresh timed out or failed. Fall back immediately to 9router's warm cache.
		fallbackURL := fmt.Sprintf("%s/api/usage/%s", baseURL, url.PathEscape(conn.ID))
		fbCtx, fbCancel := context.WithTimeout(ctx, 6*time.Second)
		fbBody, fbErr := a.doWithAuth(fbCtx, p, "GET", fallbackURL, nil)
		fbCancel()
		if fbErr == nil && json.Unmarshal(fbBody, &data) == nil && data.Error == "" {
			err = nil
			fellBackToCache = true
		}
	}

	if err != nil {
		cq.Status = "error"
		if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "Timeout exceeded") {
			cq.ErrorMsg = "Upstream provider timed out while refreshing live quota"
		} else {
			cq.ErrorMsg = err.Error()
		}
		return cq, err
	}

	if !fellBackToCache {
		if err := json.Unmarshal(respBody, &data); err != nil {
			cq.Status = "error"
			cq.ErrorMsg = fmt.Sprintf("invalid usage JSON: %v", err)
			return cq, err
		}
	}

	if data.Error != "" {
		cq.Status = "error"
		cq.ErrorMsg = data.Error
		return cq, fmt.Errorf("usage error: %s", data.Error)
	}

	cq.Plan = data.Plan

	if data.ResetCredits != nil {
		cq.ResetCredits = data.ResetCredits.AvailableCount
		if data.ResetCredits.NextGrantID != nil {
			cq.NextGrantID = *data.ResetCredits.NextGrantID
		}
		cq.ResetExpiresAt = data.ResetCredits.ExpiresAt
	}

	// Detect if summary quotas exist (e.g. Antigravity summary quotas)
	hasGeminiSummary := false
	hasClaudeSummary := false
	for k := range data.Quotas {
		kLower := strings.ToLower(k)
		if strings.Contains(kLower, "gemini_session") || strings.Contains(kLower, "gemini_weekly") {
			hasGeminiSummary = true
		}
		if strings.Contains(kLower, "claude_gpt_session") || strings.Contains(kLower, "claude_gpt_weekly") {
			hasClaudeSummary = true
		}
	}

	// Parse buckets
	for name, item := range data.Quotas {
		kLower := strings.ToLower(name)
		isImage := strings.Contains(kLower, "image")

		// Deduplicate redundant Antigravity sub-model mirrors if summary pools exist
		if hasGeminiSummary && strings.HasPrefix(kLower, "gemini-") && !isImage {
			continue
		}
		if hasClaudeSummary && (strings.HasPrefix(kLower, "claude-") || strings.HasPrefix(kLower, "gpt-oss")) {
			continue
		}

		bucket := QuotaBucket{
			Name: cleanBucketName(name, item.DisplayName),
		}

		if item.Used != nil {
			bucket.Used = int64(math.Round(*item.Used))
		}
		if item.Total != nil {
			bucket.Total = int64(math.Round(*item.Total))
		}
		if item.Remaining != nil {
			bucket.Remaining = int64(math.Round(*item.Remaining))
		} else if bucket.Total > 0 {
			bucket.Remaining = bucket.Total - bucket.Used
			if bucket.Remaining < 0 {
				bucket.Remaining = 0
			}
		}

		if item.RemainingPercentage != nil {
			pct := int(math.Round(*item.RemainingPercentage))
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
			bucket.RemainingPercentage = pct
		} else if bucket.Total > 0 {
			pct := int(math.Round((float64(bucket.Remaining) / float64(bucket.Total)) * 100))
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
			bucket.RemainingPercentage = pct
		} else if item.Unlimited {
			bucket.RemainingPercentage = 100
		}

		// Reset time
		resetStr := item.ResetAt
		if resetStr == nil {
			resetStr = item.ResetTime
		}
		if resetStr != nil && *resetStr != "" {
			if t, err := time.Parse(time.RFC3339, *resetStr); err == nil {
				bucket.ResetAt = &t
			}
		}

		bucket.Warning = bucket.RemainingPercentage < 25 && !item.Unlimited
		cq.Buckets = append(cq.Buckets, bucket)
	}

	// Credit balance bucket if available
	balance := data.Balance
	if balance == nil {
		balance = data.CreditBalance
	}
	if balance == nil {
		balance = data.Credits
	}
	if balance != nil {
		cq.Buckets = append(cq.Buckets, QuotaBucket{
			Name:            "Credit Balance",
			Remaining:       int64(*balance * 100), // cents representation
			IsCreditBalance: true,
			Warning:         *balance <= 0,
		})
	}

	sortBuckets(cq.Buckets)

	return cq, nil
}

func bucketRank(b QuotaBucket) int {
	if b.IsCreditBalance {
		return 5
	}
	if b.Warning {
		return 1
	}
	nameLower := strings.ToLower(b.Name)
	if strings.Contains(nameLower, "5h") || strings.Contains(nameLower, "session") {
		return 2
	}
	if strings.Contains(nameLower, "weekly") || strings.Contains(nameLower, "7d") {
		return 3
	}
	return 4
}

func sortBuckets(buckets []QuotaBucket) {
	sort.SliceStable(buckets, func(i, j int) bool {
		rI := bucketRank(buckets[i])
		rJ := bucketRank(buckets[j])
		if rI != rJ {
			return rI < rJ
		}
		if rI == 1 {
			if buckets[i].RemainingPercentage != buckets[j].RemainingPercentage {
				return buckets[i].RemainingPercentage < buckets[j].RemainingPercentage
			}
		}
		return buckets[i].Name < buckets[j].Name
	})
}

func cleanBucketName(key, displayName string) string {
	if strings.TrimSpace(displayName) != "" {
		return displayName
	}
	switch key {
	case "five_hour", "session", "gemini_session", "claude_gpt_session":
		return "5h Session"
	case "seven_day", "weekly", "gemini_weekly", "claude_gpt_weekly":
		return "Weekly Limit"
	default:
		// Replace underscores and title case
		k := strings.ReplaceAll(key, "_", " ")
		return strings.Title(k)
	}
}

// doWithAuth sends an HTTP request, attaching stored session cookies if present,
// and attempting authentication via /api/auth/login if 401 is encountered.
func (a *Router9Adapter) doWithAuth(ctx context.Context, p providers.Provider, method, reqURL string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// Attach existing cookies
	a.cookieMu.RLock()
	cookies := a.cookieJar[p.ID]
	a.cookieMu.RUnlock()
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Attempt login if secret configured
		if strings.TrimSpace(p.QuotaAuthSecret) != "" {
			if loginErr := a.login(ctx, p, extractBaseURL(reqURL)); loginErr == nil {
				// Retry request once with new cookies
				req2, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
				if err != nil {
					return nil, err
				}
				req2.Header.Set("Accept", "application/json")
				if len(body) > 0 {
					req2.Header.Set("Content-Type", "application/json")
				}
				a.cookieMu.RLock()
				newCookies := a.cookieJar[p.ID]
				a.cookieMu.RUnlock()
				for _, c := range newCookies {
					req2.AddCookie(c)
				}

				resp2, err := a.client.Do(req2)
				if err != nil {
					return nil, err
				}
				defer resp2.Body.Close()
				if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
					return io.ReadAll(resp2.Body)
				}
				return nil, fmt.Errorf("HTTP %d: %s", resp2.StatusCode, resp2.Status)
			}
		}
		return nil, fmt.Errorf("HTTP 401 Unauthorized (9router login required)")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (a *Router9Adapter) login(ctx context.Context, p providers.Provider, baseURL string) error {
	loginURL := baseURL + "/api/auth/login"
	payload, _ := json.Marshal(map[string]string{
		"password": p.QuotaAuthSecret,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with HTTP %d", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) > 0 {
		a.cookieMu.Lock()
		a.cookieJar[p.ID] = cookies
		a.cookieMu.Unlock()
	}

	return nil
}

// ConsumeReset triggers spending an upstream reset credit on 9router.
func (a *Router9Adapter) ConsumeReset(ctx context.Context, p providers.Provider, connectionID, providerKind, grantID string) error {
	baseURL := extractBaseURL(p.Route)
	if baseURL == "" {
		return fmt.Errorf("empty base URL for provider %s", p.ID)
	}

	endpoint := "/api/usage/" + url.PathEscape(connectionID) + "/codex-reset-credits"
	var body []byte
	if strings.EqualFold(providerKind, "claude") || strings.EqualFold(providerKind, "anthropic") {
		endpoint = "/api/usage/" + url.PathEscape(connectionID) + "/claude-reset"
		if grantID != "" {
			body, _ = json.Marshal(map[string]string{"grantId": grantID})
		}
	}

	reqURL := baseURL + endpoint
	respBytes, err := a.doWithAuth(ctx, p, "POST", reqURL, body)
	if err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(respBytes, &res); err == nil {
		if errMsg, ok := res["error"].(string); ok && errMsg != "" {
			return errors.New(errMsg)
		}
		if errMsg, ok := res["message"].(string); ok && errMsg != "" {
			if okVal, _ := res["ok"].(bool); !okVal {
				if resetVal, _ := res["reset"].(bool); !resetVal {
					return errors.New(errMsg)
				}
			}
		}
	}

	return nil
}
