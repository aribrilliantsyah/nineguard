package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/models"
	"nineguard/internal/pricing"
)

func (h *Handler) SetPricing(pm *pricing.Manager) {
	h.pricing = pm
}

func (h *Handler) GetPrices(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	allModels, err := h.models.ListModels("")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list models")
		return
	}

	resolver := h.pricing.Resolver()
	var result []pricing.ModelWithPrice

	for _, m := range allModels {
		fam := models.NormalizeFamily(m.ID)
		price, source, found := resolver.PriceFor(m.ID)
		mwp := pricing.ModelWithPrice{
			ModelID: m.ID,
			Family:  fam,
			Source:  "none",
		}
		if found && price != nil {
			mwp.Source = source
			mwp.InputPerMTok = &price.InputPerMTok
			mwp.OutputPerMTok = &price.OutputPerMTok
			mwp.CacheReadPerMTok = price.CacheReadPerMTok
			mwp.CacheWritePerMTok = price.CacheWritePerMTok
			mwp.IsOverride = (source == "override")
		}
		result = append(result, mwp)
	}

	jsonResponse(w, http.StatusOK, result)
}

func (h *Handler) SetPriceOverride(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	modelID := r.PathValue("model_id")
	if modelID == "" {
		modelID = r.PathValue("id")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		jsonError(w, http.StatusBadRequest, "model_id required")
		return
	}

	var body struct {
		InputPerMTok      *float64 `json:"input_per_mtok"`
		OutputPerMTok     *float64 `json:"output_per_mtok"`
		CacheReadPerMTok  *float64 `json:"cache_read_per_mtok"`
		CacheWritePerMTok *float64 `json:"cache_write_per_mtok"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if body.InputPerMTok == nil || *body.InputPerMTok < 0 {
		jsonError(w, http.StatusBadRequest, "input_per_mtok must be a non-negative number")
		return
	}
	if body.OutputPerMTok == nil || *body.OutputPerMTok < 0 {
		jsonError(w, http.StatusBadRequest, "output_per_mtok must be a non-negative number")
		return
	}

	if err := h.pricing.SetOverride(modelID, *body.InputPerMTok, *body.OutputPerMTok, body.CacheReadPerMTok, body.CacheWritePerMTok); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	recalculated := 0
	if rec, err := pricing.RepriceRange(r.Context(), h.pricing.DB(), h.pricing.Resolver(), time.Time{}, time.Time{}, modelID); err == nil {
		recalculated = rec
	}

	jsonResponse(w, http.StatusOK, map[string]any{"status": "ok", "model_id": modelID, "recalculated": recalculated})
}

func (h *Handler) DeletePriceOverride(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	modelID := r.PathValue("model_id")
	if modelID == "" {
		modelID = r.PathValue("id")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		jsonError(w, http.StatusBadRequest, "model_id required")
		return
	}

	if err := h.pricing.DeleteOverride(modelID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	recalculated := 0
	if rec, err := pricing.RepriceRange(r.Context(), h.pricing.DB(), h.pricing.Resolver(), time.Time{}, time.Time{}, modelID); err == nil {
		recalculated = rec
	}

	jsonResponse(w, http.StatusOK, map[string]any{"status": "ok", "deleted": modelID, "recalculated": recalculated})
}

func (h *Handler) RefreshPrices(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	count, err := h.pricing.RefreshPrices(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh prices: %v", err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"count":     count,
		"synced_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) GetCostSettings(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	settings, err := h.pricing.GetCostSettings()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, settings)
}

func (h *Handler) UpdateCostSettings(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	var body pricing.CostSettingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if err := h.pricing.UpdateCostSettings(body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	settings, err := h.pricing.GetCostSettings()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, settings)
}

func (h *Handler) RepriceCosts(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		jsonError(w, http.StatusBadRequest, "Pricing manager not available")
		return
	}

	var body struct {
		From    string `json:"from"`
		To      string `json:"to"`
		ModelID string `json:"model_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var from, to time.Time
	if body.From != "" {
		if t, err := time.Parse(time.RFC3339, body.From); err == nil {
			from = t
		} else if t, err := time.Parse("2006-01-02", body.From); err == nil {
			from = t
		}
	}
	if body.To != "" {
		if t, err := time.Parse(time.RFC3339, body.To); err == nil {
			to = t
		} else if t, err := time.Parse("2006-01-02", body.To); err == nil {
			to = t.Add(24*time.Hour - time.Nanosecond)
		}
	}

	updated, err := pricing.RepriceRange(r.Context(), h.pricing.DB(), h.pricing.Resolver(), from, to, body.ModelID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"updated": updated})
}
