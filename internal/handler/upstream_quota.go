package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/providers"
	"nineguard/internal/upstream"
)

// GetUpstreamQuotas returns diagnostic quota information for all configured providers (Admin API).
func (h *Handler) GetUpstreamQuotas(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	if h.upstream == nil {
		h.upstream = upstream.NewManager()
	}

	force := r.URL.Query().Get("force") == "1"
	provList, err := h.providers.ListProviders()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list providers")
		return
	}

	type provResult struct {
		res *upstream.ProviderQuotaResult
		err error
	}

	resultsChan := make(chan provResult, len(provList))
	var wg sync.WaitGroup

	for _, p := range provList {
		wg.Add(1)
		go func(provCopy providers.Provider) {
			defer wg.Done()
			res, fetchErr := h.upstream.GetProviderQuotas(r.Context(), provCopy, force)
			resultsChan <- provResult{res: res, err: fetchErr}
		}(p)
	}

	wg.Wait()
	close(resultsChan)

	results := make([]*upstream.ProviderQuotaResult, 0, len(provList))
	for pr := range resultsChan {
		if pr.res != nil {
			results = append(results, pr.res)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"providers": results,
	})
}

// BuildUnifiedQuotaResponse constructs client-facing merged quota information filtered by key permissions.
func (h *Handler) BuildUnifiedQuotaResponse(ctx context.Context, keyInfo *keys.KeyInfo, force bool) (*upstream.UnifiedQuotaResponse, error) {
	if h.upstream == nil {
		h.upstream = upstream.NewManager()
	}

	resp := &upstream.UnifiedQuotaResponse{
		UpstreamPools: make([]upstream.MergedPoolQuota, 0),
	}

	now := time.Now()

	// 1. Client key budget
	if keyInfo != nil {
		if !keyInfo.EnforcesModelQuotas() && keyInfo.QuotaLimit > 0 && keyInfo.QuotaPeriod != "" && keyInfo.QuotaPeriod != "none" {
			if h.traffic != nil {
				consumed, resetAt, err := h.traffic.GetQuotaUsage(keyInfo.ID, keyInfo.QuotaPeriod, keyInfo.ResetRule, now)
				if err == nil {
					remaining := keyInfo.QuotaLimit - consumed
					if remaining < 0 {
						remaining = 0
					}
					var resetTime *time.Time
					if !resetAt.IsZero() {
						resetTime = &resetAt
					}
					resp.KeyQuota = &upstream.ClientKeyBudget{
						Mode:      "key",
						Limit:     keyInfo.QuotaLimit,
						Used:      consumed,
						Remaining: remaining,
						Period:    keyInfo.QuotaPeriod,
						ResetAt:   resetTime,
						Warning:   consumed >= int64(float64(keyInfo.QuotaLimit)*0.8),
					}
				}
			}
		}
	}

	// 2. Fetch active upstream providers and merge pools
	if h.providers != nil {
		provList, err := h.providers.ListProviders()
		if err == nil {
			for _, prov := range provList {
				if !prov.IsActive || prov.QuotaType == "disabled" || prov.QuotaType == "" {
					continue
				}

				res, fetchErr := h.upstream.GetProviderQuotas(ctx, prov, force)
				if fetchErr == nil && res != nil && len(res.Connections) > 0 {
					mergedPools := upstream.MergeAccountPools(prov.Name, res.Connections)
					resp.UpstreamPools = append(resp.UpstreamPools, mergedPools...)
				}
			}
		}
	}

	// 3. Filter pools by model access if key has restrictions
	if keyInfo != nil && !keyInfo.IsAllModelsAllowed() {
		resp.UpstreamPools = upstream.FilterPoolsByModelAccess(resp.UpstreamPools, keyInfo.AllowedModels)
	}

	return resp, nil
}

func (h *Handler) SetUpstreamManager(um *upstream.Manager) {
	h.upstream = um
}

// currentSyncInterval reads the stored sync interval, falling back to the default when invalid.
func (h *Handler) currentSyncInterval() time.Duration {
	if h.traffic == nil {
		return upstream.DefaultSyncInterval
	}
	d, err := upstream.ParseSyncInterval(h.traffic.GetUpstreamSyncInterval())
	if err != nil {
		return upstream.DefaultSyncInterval
	}
	return d
}

// applySyncInterval aligns the shared cache TTL with the sync interval (interval + 5m).
func (h *Handler) applySyncInterval(d time.Duration) {
	if h.upstream != nil {
		h.upstream.SetCacheTTL(upstream.CacheTTLForInterval(d))
	}
}

// RunUpstreamQuotaSync is the background worker: warms the cache shortly after startup, then
// refreshes on the configured interval. Saving a new interval restarts the wait without a restart.
func (h *Handler) RunUpstreamQuotaSync(ctx context.Context) {
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}
	h.applySyncInterval(h.currentSyncInterval())
	if n := h.SyncAllUpstreamQuotas(ctx, false); n > 0 {
		slog.Info("warmed upstream quota cache in background", "providers", n)
	}

	for {
		interval := h.currentSyncInterval()
		h.applySyncInterval(interval)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-h.syncReset:
			timer.Stop()
		case <-timer.C:
			h.SyncAllUpstreamQuotas(ctx, true)
		}
	}
}

// GetUpstreamSyncSettings returns the current auto-sync interval and the allowed choices (Admin API).
func (h *Handler) GetUpstreamSyncSettings(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, h.syncSettingsPayload())
}

// SetUpstreamSyncSettings validates and stores the auto-sync interval, then applies it live (Admin API).
func (h *Handler) SetUpstreamSyncSettings(w http.ResponseWriter, r *http.Request) {
	if h.traffic == nil {
		jsonError(w, http.StatusBadRequest, "Traffic manager not available")
		return
	}
	var body struct {
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	d, err := upstream.ParseSyncInterval(body.Interval)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.traffic.SetUpstreamSyncInterval(upstream.FormatInterval(d)); err != nil {
		slog.Error("failed to save upstream sync interval", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to save sync interval")
		return
	}
	h.applySyncInterval(d)
	select {
	case h.syncReset <- struct{}{}:
	default:
	}
	jsonResponse(w, http.StatusOK, h.syncSettingsPayload())
}

func (h *Handler) syncSettingsPayload() map[string]interface{} {
	allowed := make([]string, 0, len(upstream.AllowedSyncIntervals))
	for _, a := range upstream.AllowedSyncIntervals {
		allowed = append(allowed, upstream.FormatInterval(a))
	}
	return map[string]interface{}{
		"interval": upstream.FormatInterval(h.currentSyncInterval()),
		"allowed":  allowed,
		"default":  upstream.FormatInterval(upstream.DefaultSyncInterval),
	}
}

// SyncAllUpstreamQuotas refreshes quotas for all active providers in parallel (used by background worker).
func (h *Handler) SyncAllUpstreamQuotas(ctx context.Context, force bool) int {
	if h.providers == nil || h.upstream == nil {
		return 0
	}
	provList, err := h.providers.ListProviders()
	if err != nil {
		return 0
	}

	var wg sync.WaitGroup
	syncedCount := 0
	var mu sync.Mutex

	for _, p := range provList {
		if !p.IsActive || p.QuotaType == "disabled" || p.QuotaType == "" {
			continue
		}
		wg.Add(1)
		go func(provCopy providers.Provider) {
			defer wg.Done()
			res, fetchErr := h.upstream.GetProviderQuotas(ctx, provCopy, force)
			if fetchErr == nil && res != nil && res.Status == "ok" {
				mu.Lock()
				syncedCount++
				mu.Unlock()
			}
		}(p)
	}

	wg.Wait()
	return syncedCount
}

// ConsumeUpstreamReset triggers spending an upstream reset grant/credit on 9router.
func (h *Handler) ConsumeUpstreamReset(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	if h.upstream == nil {
		h.upstream = upstream.NewManager()
	}

	providerID := r.PathValue("id")
	connID := r.PathValue("connId")

	p, err := h.providers.GetProvider(providerID)
	if err != nil || p == nil {
		jsonError(w, http.StatusNotFound, "Provider not found")
		return
	}

	var body struct {
		ProviderKind string `json:"provider"`
		GrantID      string `json:"grant_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	err = h.upstream.ConsumeReset(r.Context(), *p, connID, body.ProviderKind, body.GrantID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Reset applied successfully",
	})
}
