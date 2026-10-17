package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/providers"
)

// NineGuardAdapter communicates with a cascaded NineGuard instance.
type NineGuardAdapter struct {
	client     *http.Client
	instanceID string
}

func NewNineGuardAdapter() *NineGuardAdapter {
	return &NineGuardAdapter{
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (a *NineGuardAdapter) SetInstanceID(id string) {
	a.instanceID = id
}

// FetchQuotas calls GET /v1/quota on the upstream NineGuard instance.
func (a *NineGuardAdapter) FetchQuotas(ctx context.Context, p providers.Provider, force bool) (*ProviderQuotaResult, error) {
	baseURL := extractBaseURL(p.Route)
	if baseURL == "" {
		return nil, fmt.Errorf("empty base URL for provider %s", p.ID)
	}

	res := &ProviderQuotaResult{
		ProviderID:   p.ID,
		ProviderName: p.Name,
		QuotaType:    "nineguard",
		Status:       "ok",
		LastUpdated:  time.Now(),
		Connections:  make([]ConnectionQuota, 0),
	}

	reqURL := baseURL + "/v1/quota"
	if force {
		reqURL += "?force=1"
	}

	callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, "GET", reqURL, nil)
	if err != nil {
		res.Status = "error"
		res.ErrorMsg = err.Error()
		return res, nil
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(p.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if a.instanceID != "" {
		req.Header.Set("X-NineGuard-Trace", a.instanceID)
		req.Header.Set("X-NineGuard-Hop", "1")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		res.Status = "error"
		if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "Timeout exceeded") {
			res.ErrorMsg = "cascaded NineGuard instance timed out while refreshing quotas"
		} else {
			res.ErrorMsg = fmt.Sprintf("failed to contact upstream NineGuard: %v", err)
		}
		return res, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.Status = "error"
		res.ErrorMsg = fmt.Sprintf("upstream NineGuard returned HTTP %d: %s", resp.StatusCode, resp.Status)
		return res, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		res.Status = "error"
		res.ErrorMsg = fmt.Sprintf("failed to read response body: %v", err)
		return res, nil
	}

	var upstreamData UnifiedQuotaResponse
	if err := json.Unmarshal(body, &upstreamData); err != nil {
		res.Status = "error"
		res.ErrorMsg = fmt.Sprintf("failed to parse upstream NineGuard quota JSON: %v", err)
		return res, nil
	}

	// 1. If upstream returns personal KeyQuota or ModelQuotas, synthesize an allowance connection
	if upstreamData.KeyQuota != nil {
		kq := upstreamData.KeyQuota
		var pct int
		if kq.Limit > 0 {
			pct = int((float64(kq.Remaining) / float64(kq.Limit)) * 100)
		}
		res.Connections = append(res.Connections, ConnectionQuota{
			ConnectionID: "company_key_allowance",
			Provider:     "nineguard",
			AccountLabel: "Key Allowance (Admin Limit)",
			Plan:         "Client Key Budget",
			Status:       "ok",
			LastUpdated:  time.Now(),
			Buckets: []QuotaBucket{
				{
					Name:                fmt.Sprintf("Key Allowance (%s)", kq.Period),
					Used:                kq.Used,
					Total:               kq.Limit,
					Remaining:           kq.Remaining,
					RemainingPercentage: pct,
					ResetAt:             kq.ResetAt,
					Warning:             kq.Warning,
				},
			},
		})
	} else if len(upstreamData.ModelQuotas) > 0 {
		var buckets []QuotaBucket
		for _, mq := range upstreamData.ModelQuotas {
			var pct int
			if mq.Limit > 0 {
				pct = int((float64(mq.Remaining) / float64(mq.Limit)) * 100)
			}
			buckets = append(buckets, QuotaBucket{
				Name:                fmt.Sprintf("%s (%s)", mq.ModelID, mq.Period),
				Used:                mq.Used,
				Total:               mq.Limit,
				Remaining:           mq.Remaining,
				RemainingPercentage: pct,
				ResetAt:             mq.ResetAt,
				Warning:             mq.Warning,
			})
		}
		sortBuckets(buckets)
		res.Connections = append(res.Connections, ConnectionQuota{
			ConnectionID: "company_model_allowances",
			Provider:     "nineguard",
			AccountLabel: "Key Allowance (Admin Limit)",
			Plan:         "Per-Model Budgets",
			Status:       "ok",
			LastUpdated:  time.Now(),
			Buckets:      buckets,
		})
	}

	// 2. Include all upstream pools as connections
	for _, pool := range upstreamData.UpstreamPools {
		buckets := make([]QuotaBucket, len(pool.Buckets))
		copy(buckets, pool.Buckets)
		sortBuckets(buckets)

		sub := pool.SubProvider
		if sub == "" && strings.Contains(pool.Family, " · ") {
			parts := strings.Split(pool.Family, " · ")
			if len(parts) > 1 {
				sub = strings.TrimSpace(parts[len(parts)-1])
			}
		}
		if sub == "" {
			sub = pool.Provider
		}

		connLabel := pool.Family
		if connLabel == "" {
			connLabel = pool.Provider
		}

		res.Connections = append(res.Connections, ConnectionQuota{
			ConnectionID: pool.Family,
			Provider:     sub,
			AccountLabel: connLabel,
			Plan:         "Shared Upstream Pool",
			Status:       pool.Status,
			LastUpdated:  time.Now(),
			Buckets:      buckets,
		})
	}

	return res, nil
}
