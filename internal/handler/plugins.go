package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
)

type PluginWithMeta struct {
	plugins.Plugin
	GlobalBinding *plugins.Binding  `json:"global_binding,omitempty"`
	Guidance      *plugins.Guidance `json:"guidance,omitempty"`
}

// ListPlugins returns all plugins with masked secrets, global binding, and guidance.
func (h *Handler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}

	list, err := h.plugins.ListPlugins()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := make([]PluginWithMeta, len(list))
	for i, p := range list {
		meta := PluginWithMeta{Plugin: p}
		if g, ok := plugins.BuiltinGuidance[p.ID]; ok {
			meta.Guidance = &g
		}
		if bindings, err := h.plugins.ListBindings(p.ID); err == nil {
			for _, b := range bindings {
				if b.ScopeType == plugins.ScopeGlobal {
					bCopy := b
					meta.GlobalBinding = &bCopy
					break
				}
			}
		}
		result[i] = meta
	}

	jsonResponse(w, http.StatusOK, map[string]any{"plugins": result})
}

// CreatePlugin registers an HTTP plugin (admin only).
func (h *Handler) CreatePlugin(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can register plugins")
		return
	}

	var body struct {
		Name            string                `json:"name"`
		Description     string                `json:"description"`
		URL             string                `json:"url"`
		TimeoutMs       int                   `json:"timeout_ms"`
		FailurePolicy   plugins.FailurePolicy `json:"failure_policy"`
		Bypassable      bool                  `json:"bypassable"`
		Category        plugins.Category      `json:"category"`
		Summary         string                `json:"summary"`
		DefaultSettings string                `json:"default_settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	p := &plugins.Plugin{
		Name:            body.Name,
		Description:     body.Description,
		URL:             body.URL,
		TimeoutMs:       body.TimeoutMs,
		FailurePolicy:   body.FailurePolicy,
		Bypassable:      body.Bypassable,
		Category:        body.Category,
		Summary:         body.Summary,
		DefaultSettings: body.DefaultSettings,
	}

	created, rawSecret, err := h.plugins.CreateHTTPPlugin(p)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logAudit(r, user.ID, user.Username, "plugin.create", created.ID, "success", "name="+created.Name)
	jsonResponse(w, http.StatusCreated, map[string]any{
		"plugin": created,
		"secret": rawSecret,
	})
}

// UpdatePlugin updates plugin fields (admin for url/secret/failure_policy; operator for rest).
func (h *Handler) UpdatePlugin(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	id := r.PathValue("id")
	user := auth.UserFromContext(r.Context())

	var body struct {
		Name            string                `json:"name"`
		Description     string                `json:"description"`
		URL             *string               `json:"url"`
		TimeoutMs       int                   `json:"timeout_ms"`
		FailurePolicy   plugins.FailurePolicy `json:"failure_policy"`
		Bypassable      bool                  `json:"bypassable"`
		Category        plugins.Category      `json:"category"`
		Summary         string                `json:"summary"`
		DefaultSettings string                `json:"default_settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if (body.URL != nil || body.FailurePolicy != "") && (user == nil || user.Role != "admin") {
		jsonError(w, http.StatusForbidden, "Only administrators can update URL or failure policy")
		return
	}

	urlStr := ""
	if body.URL != nil {
		urlStr = *body.URL
	}

	p := &plugins.Plugin{
		ID:              id,
		Name:            body.Name,
		Description:     body.Description,
		URL:             urlStr,
		TimeoutMs:       body.TimeoutMs,
		FailurePolicy:   body.FailurePolicy,
		Bypassable:      body.Bypassable,
		Category:        body.Category,
		Summary:         body.Summary,
		DefaultSettings: body.DefaultSettings,
	}

	if err := h.plugins.UpdatePlugin(p); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var actorID int64
	var username string
	if user != nil {
		actorID = user.ID
		username = user.Username
	}
	h.logAudit(r, actorID, username, "plugin.update", id, "success", "")

	updated, _ := h.plugins.GetPlugin(id)
	jsonResponse(w, http.StatusOK, updated)
}

// RotatePluginSecret generates and returns a new secret for an HTTP plugin (admin only).
func (h *Handler) RotatePluginSecret(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can rotate plugin secrets")
		return
	}
	id := r.PathValue("id")

	newSecret, err := h.plugins.RotateSecret(id)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logAudit(r, user.ID, user.Username, "plugin.rotate_secret", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"secret": newSecret})
}

// DeletePlugin removes an HTTP plugin (admin only). Built-in plugins cannot be deleted.
func (h *Handler) DeletePlugin(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can delete plugins")
		return
	}
	id := r.PathValue("id")

	if err := h.plugins.DeletePlugin(id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logAudit(r, user.ID, user.Username, "plugin.delete", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TestPlugin verifies plugin connectivity with a test request and measures latency.
func (h *Handler) TestPlugin(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	id := r.PathValue("id")

	pRaw, err := h.plugins.GetPluginRaw(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}

	if id == "headroom" {
		url := "http://127.0.0.1:8787"
		token := ""
		var sMap map[string]any
		_ = json.Unmarshal([]byte(pRaw.DefaultSettings), &sMap)
		if u, ok := sMap["url"].(string); ok && strings.TrimSpace(u) != "" {
			url = strings.TrimRight(strings.TrimSpace(u), "/")
		}
		if t, ok := sMap["token"].(string); ok && strings.TrimSpace(t) != "" {
			token = strings.TrimSpace(t)
		}

		dummyCompress := map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": "ping"},
			},
			"model": "ping",
			"config": map[string]any{
				"frozen_message_count":   0,
				"compress_user_messages": false,
			},
		}
		b, _ := json.Marshal(dummyCompress)
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/compress", bytes.NewReader(b))
		if err != nil {
			jsonResponse(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error(), "latency_ms": 0})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			jsonResponse(w, http.StatusBadGateway, map[string]any{
				"status":     "error",
				"error":      fmt.Sprintf("Cannot reach Headroom at %s: %v", url, err),
				"latency_ms": latencyMs,
			})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			jsonResponse(w, http.StatusBadGateway, map[string]any{
				"status":     "error",
				"error":      fmt.Sprintf("Headroom at %s returned HTTP %d", url, resp.StatusCode),
				"latency_ms": latencyMs,
			})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"status": "ok", "latency_ms": latencyMs})
		return
	}

	if pRaw.Kind == plugins.KindBuiltin {
		jsonResponse(w, http.StatusOK, map[string]any{"status": "ok", "latency_ms": 0, "in_process": true})
		return
	}

	// For HTTP plugin: send dummy chat completion request
	status, body := probeHTTPPlugin(r.Context(), pRaw.URL, pRaw.Secret, pRaw.TimeoutMs)
	jsonResponse(w, status, body)
}

// TestPluginURL checks that an endpoint answers before it is registered (admin only).
// No secret exists yet, so none is sent; a plugin that requires one may answer 401/403.
func (h *Handler) TestPluginURL(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can test plugin endpoints")
		return
	}
	var body struct {
		URL       string `json:"url"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	url := strings.TrimSpace(body.URL)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		jsonError(w, http.StatusBadRequest, "plugin URL must use http or https")
		return
	}
	status, res := probeHTTPPlugin(r.Context(), url, "", body.TimeoutMs)
	jsonResponse(w, status, res)
}

// probeHTTPPlugin sends a dummy chat completion to an HTTP plugin and reports reachability and latency.
func probeHTTPPlugin(parent context.Context, url, secret string, timeoutMs int) (int, map[string]any) {
	dummyPayload := map[string]any{
		"request": map[string]any{
			"model": "test-model",
			"messages": []map[string]any{
				{"role": "user", "content": "ping"},
			},
		},
		"context": plugins.HTTPPluginContext{
			APIKeyName: "test-runner",
			Model:      "test-model",
			Provider:   "test",
		},
	}
	b, _ := json.Marshal(dummyPayload)

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return http.StatusBadRequest, map[string]any{"status": "error", "error": fmt.Sprintf("invalid plugin URL: %v", err), "latency_ms": 0}
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-NineGuard-Plugin-Secret", secret)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error(), "latency_ms": latencyMs}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return http.StatusBadGateway, map[string]any{
			"status":     "error",
			"error":      fmt.Sprintf("plugin returned HTTP %d", resp.StatusCode),
			"latency_ms": latencyMs,
		}
	}
	return http.StatusOK, map[string]any{"status": "ok", "latency_ms": latencyMs}
}

// UpdatePipelineOrder updates the global execution order of plugins.
func (h *Handler) UpdatePipelineOrder(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}

	var body struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.plugins.UpdatePipelineOrder(body.Order); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "plugin.order.update", "pipeline", "success", strings.Join(body.Order, ","))
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListPluginBindings returns all bindings for a given plugin ID.
func (h *Handler) ListPluginBindings(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	id := r.PathValue("id")

	bindings, err := h.plugins.ListBindings(id)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"bindings": bindings})
}

// ListScopeBindings returns all active bindings for a specific scope (key or group).
func (h *Handler) ListScopeBindings(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	scopeType := plugins.ScopeType(r.URL.Query().Get("scope_type"))
	scopeID := r.URL.Query().Get("scope_id")

	bindings, err := h.plugins.ListBindingsForScope(scopeType, scopeID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"bindings": bindings})
}

// UpsertPluginBinding creates or updates a binding and returns any scope warnings.
func (h *Handler) UpsertPluginBinding(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	id := r.PathValue("id")

	var body struct {
		ScopeType plugins.ScopeType    `json:"scope_type"`
		ScopeID   string               `json:"scope_id"`
		State     plugins.BindingState `json:"state"`
		Settings  string               `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	b := plugins.Binding{
		PluginID:  id,
		ScopeType: body.ScopeType,
		ScopeID:   body.ScopeID,
		State:     body.State,
		Settings:  body.Settings,
	}

	if err := h.plugins.UpsertBinding(b); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "plugin.binding.update", id+":"+string(body.ScopeType)+":"+body.ScopeID, "success", "state="+string(body.State))

	// Compute scope warnings
	warnings := h.computeScopeWarnings(b)

	jsonResponse(w, http.StatusOK, map[string]any{
		"binding":  b,
		"warnings": warnings,
	})
}

func (h *Handler) computeScopeWarnings(b plugins.Binding) []plugins.Warning {
	if b.State != plugins.StateOn {
		return nil
	}

	var groups []models.ModelGroup
	if h.models != nil {
		groups, _ = h.models.ListGroups()
	}

	var provList []string
	if h.providers != nil {
		provs, _ := h.providers.ListProviders()
		for _, p := range provs {
			if p.UpstreamTokenSaving {
				provList = append(provList, p.Name)
			}
		}
	}

	resolved := h.plugins.ResolveEffective(groups, "", "", "")
	return plugins.ComputeWarnings(resolved, len(provList) > 0, "upstream token saving enabled", strings.Join(provList, ", "))
}

// ResolvePlugins previews effective plugins, settings, decided_by, overridden, and warnings for a key + model.
func (h *Handler) ResolvePlugins(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}

	keyID := r.URL.Query().Get("key_id")
	model := r.URL.Query().Get("model")

	keyName := keyID
	if h.keys != nil && keyID != "" {
		if k, err := h.keys.GetKey(keyID); err == nil && k != nil {
			keyName = k.Name
		}
	}

	var groups []models.ModelGroup
	if h.models != nil {
		groups, _ = h.models.ListGroups()
	}

	effective := h.plugins.ResolveEffective(groups, keyID, keyName, model)

	var warnings []plugins.Warning
	if h.providers != nil && model != "" {
		if prov, _, err := h.providers.FindProviderForModel(model); err == nil && prov != nil {
			warnings = plugins.ComputeWarnings(effective, prov.UpstreamTokenSaving, prov.UpstreamTokenSavingNote, prov.Name)
		}
	} else {
		warnings = plugins.ComputeWarnings(effective, false, "", "")
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"plugins":  effective,
		"warnings": warnings,
	})
}

// GetPluginWarnings evaluates active Overlap Warnings across keys and models with a 50k pair cap.
func (h *Handler) GetPluginWarnings(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}

	var groups []models.ModelGroup
	if h.models != nil {
		groups, _ = h.models.ListGroups()
	}

	var activeKeys []struct {
		id   string
		name string
	}
	if h.keys != nil {
		keysList, err := h.keys.ListKeys()
		if err == nil {
			for _, k := range keysList {
				if k.IsActive {
					activeKeys = append(activeKeys, struct {
						id   string
						name string
					}{id: k.ID, name: k.Name})
				}
			}
		}
	}

	var enabledModels []string
	if h.models != nil {
		allModels, err := h.models.ListModels("")
		if err == nil {
			for _, m := range allModels {
				if m.Enabled {
					enabledModels = append(enabledModels, m.ID)
				}
			}
		}
	}

	type WarningEntry struct {
		Code         string   `json:"code"`
		Message      string   `json:"message"`
		Plugins      []string `json:"plugins"`
		Provider     string   `json:"provider,omitempty"`
		KeyID        string   `json:"key_id"`
		KeyName      string   `json:"key_name"`
		ModelCount   int      `json:"model_count"`
		ModelsSample []string `json:"models_sample"`
	}

	evaluatedPairs := 0
	truncated := false
	const maxPairs = 50000

	groupedWarnings := make(map[string]*WarningEntry)

	for _, k := range activeKeys {
		for _, m := range enabledModels {
			evaluatedPairs++
			if evaluatedPairs > maxPairs {
				truncated = true
				break
			}

			effective := h.plugins.ResolveEffective(groups, k.id, k.name, m)
			var provIsTokenSaving bool
			var provNote, provName string
			if h.providers != nil {
				if prov, _, err := h.providers.FindProviderForModel(m); err == nil && prov != nil {
					provIsTokenSaving = prov.UpstreamTokenSaving
					provNote = prov.UpstreamTokenSavingNote
					provName = prov.Name
				}
			}

			pairWarnings := plugins.ComputeWarnings(effective, provIsTokenSaving, provNote, provName)
			for _, wItem := range pairWarnings {
				pluginsKey := strings.Join(wItem.Plugins, ",")
				groupKey := fmt.Sprintf("%s|%s|%s|%s", k.id, wItem.Code, pluginsKey, wItem.Provider)

				if entry, exists := groupedWarnings[groupKey]; exists {
					entry.ModelCount++
					if len(entry.ModelsSample) < 5 {
						entry.ModelsSample = append(entry.ModelsSample, m)
					}
				} else {
					groupedWarnings[groupKey] = &WarningEntry{
						Code:         wItem.Code,
						Message:      wItem.Message,
						Plugins:      wItem.Plugins,
						Provider:     wItem.Provider,
						KeyID:        k.id,
						KeyName:      k.name,
						ModelCount:   1,
						ModelsSample: []string{m},
					}
				}
			}
		}
		if truncated {
			break
		}
	}

	var results []WarningEntry
	for _, entry := range groupedWarnings {
		results = append(results, *entry)
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"warnings":  results,
		"truncated": truncated,
	})
}

// ResetPromptOverride clears custom prompt overrides from a plugin.
func (h *Handler) ResetPromptOverride(w http.ResponseWriter, r *http.Request) {
	if h.plugins == nil {
		jsonError(w, http.StatusBadRequest, "Plugins manager not available")
		return
	}
	id := r.PathValue("id")

	if err := h.plugins.ResetPromptOverride(id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "plugin.update", id, "success", "reset_prompt")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
