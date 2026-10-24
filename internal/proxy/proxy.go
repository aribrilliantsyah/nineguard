package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/pricing"
	"nineguard/internal/providers"
	"nineguard/internal/traffic"
	"nineguard/internal/upstream"
)

type Proxy struct {
	models     *models.Manager
	traffic    *traffic.Manager
	keys       *keys.Manager
	providers  *providers.Manager
	plugins    *plugins.Manager
	upstream   *upstream.Manager
	instanceID string
	httpClient *http.Client
	fallbackAlerter *FallbackAlerter
	pricingResolver *pricing.Resolver
	pricingManager  *pricing.Manager
}

func (p *Proxy) SetPricingResolver(r *pricing.Resolver) {
	p.pricingResolver = r
}

func (p *Proxy) SetPricing(pm *pricing.Manager) {
	p.pricingManager = pm
	if pm != nil {
		p.pricingResolver = pm.Resolver()
	}
}

func (p *Proxy) SetInstanceID(id string) {
	p.instanceID = id
}

func (p *Proxy) SetUpstreamManager(um *upstream.Manager) {
	p.upstream = um
}

func NewProxy(mm *models.Manager, tm *traffic.Manager, km *keys.Manager, pm *providers.Manager, plm *plugins.Manager) (*Proxy, error) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Minute, // Upper bound protection for long streaming
	}
	return &Proxy{
		models:     mm,
		traffic:    tm,
		keys:       km,
		providers:  pm,
		plugins:         plm,
		upstream:        upstream.NewManager(),
		fallbackAlerter: NewFallbackAlerter(10 * time.Minute),
		httpClient:      client,
	}, nil
}

type chatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

func detectImages(messages []chatMessage) (bool, int) {
	count := 0
	for _, m := range messages {
		switch parts := m.Content.(type) {
		case []interface{}:
			for _, part := range parts {
				if obj, ok := part.(map[string]interface{}); ok {
					pType, _ := obj["type"].(string)
					if pType == "image" || pType == "image_url" || pType == "input_image" || obj["image_url"] != nil {
						count++
					}
				}
			}
		}
	}
	return count > 0, count
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Usage openAIUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func mapStatusToAnthropicType(statusCode int, errType string) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusInternalServerError:
		return "api_error"
	default:
		if errType != "" {
			return errType
		}
		return "api_error"
	}
}

func writeProxyError(w http.ResponseWriter, r *http.Request, statusCode int, errType, errCode, errMsg, param string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	format := DetectAPIFormat(r.URL.Path)
	if format == "messages" {
		anthropicType := mapStatusToAnthropicType(statusCode, errType)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    anthropicType,
				"message": errMsg,
			},
		})
		return
	}

	errObj := map[string]any{
		"message": errMsg,
		"type":    errType,
		"code":    errCode,
	}
	if param != "" {
		errObj["param"] = param
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": errObj,
	})
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		clientIP = strings.Split(forwarded, ",")[0]
	}

	// 0. Loop Protection (Recursion Guard)
	traceHeader := r.Header.Get("X-NineGuard-Trace")
	hopHeader := r.Header.Get("X-NineGuard-Hop")
	hopCount := 0
	if hopHeader != "" {
		if h, err := strconv.Atoi(hopHeader); err == nil {
			hopCount = h
		}
	}

	if (p.instanceID != "" && traceHeader != "" && strings.Contains(traceHeader, p.instanceID)) || hopCount >= 3 {
		slog.Error("loop detected: recursive request aborted", "instance_id", p.instanceID, "trace", traceHeader, "hop", hopCount)
		writeProxyError(w, r, 508, "routing_error", "loop_detected", "Loop detected: request was routed recursively back to the same NineGuard instance or exceeded maximum hop count (3).", "")
		return
	}

	// 1. Strict Gatekeeper: Validate Client NineGuard API Key
	clientAuth := r.Header.Get("Authorization")
	if clientAuth == "" {
		clientAuth = r.Header.Get("x-api-key")
	}
	var keyInfo *keys.KeyInfo
	var valid bool
	if p.keys != nil {
		keyInfo, valid = p.keys.ValidateClientKey(clientAuth)
	}

	if !valid {
		slog.Warn("unauthorized proxy request: invalid api key", "ip", clientIP)
		errMsg := "invalid or inactive API key"
		if p.traffic != nil {
			_ = p.traffic.Record(&traffic.LogEntry{
				APIKey:       "unauthorized",
				APIKeyName:   "Unknown",
				Model:        "unknown",
				DurationMs:   int(time.Since(start).Milliseconds()),
				StatusCode:   http.StatusUnauthorized,
				ClientIP:     clientIP,
				ErrorMessage: &errMsg,
				Level:        "WARN",
				ApiFormat:    DetectAPIFormat(r.URL.Path),
			})
		}

		writeProxyError(w, r, http.StatusUnauthorized, "authentication_error", "invalid_api_key", "Invalid or inactive NineGuard API key. Please generate an API key in the NineGuard dashboard (Endpoints & Keys).", "")
		return
	}

	keyName := keyInfo.Name
	maskedKey := keyInfo.Key

	// 2. Handle GET /v1/models: Aggregate models across all active OpenAI-compatible providers
	if r.Method == http.MethodGet && (r.URL.Path == "/v1/models" || r.URL.Path == "/models") {
		if p.providers != nil {
			modelsList, err := p.providers.AggregateModels(r.Context())
			if err == nil {
				dataList := modelsList
				if keyInfo != nil && !keyInfo.IsAllModelsAllowed() {
					filtered := make([]map[string]interface{}, 0)
					for _, mItem := range modelsList {
						mID, _ := mItem["id"].(string)
						if keyInfo.IsModelAllowed(mID) {
							filtered = append(filtered, mItem)
						}
					}
					dataList = filtered
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"object": "list",
					"data":   dataList,
				})
				return
			}
		}
	}

	// 2.1 Handle GET /v1/quota: Unified client token budget and upstream capacity
	if r.Method == http.MethodGet && (r.URL.Path == "/v1/quota" || r.URL.Path == "/quota") {
		force := r.URL.Query().Get("force") == "1"
		resp := p.buildUnifiedQuotaResponse(r.Context(), keyInfo, force)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// 2.2 Handle GET /v1/usage: Client token and cost usage summary
	if r.Method == http.MethodGet && (r.URL.Path == "/v1/usage" || r.URL.Path == "/usage") {
		p.handleGetUsage(w, r, keyInfo, maskedKey, keyName, clientIP, start)
		return
	}

	apiFormat := DetectAPIFormat(r.URL.Path)

	// 2.5 Enforce the Key Quota (ADR 0005). Keys in model mode skip it: their Model Quotas
	// are checked after the model is known (ADR 0007).
	if p.traffic != nil && keyInfo != nil && !keyInfo.EnforcesModelQuotas() && keyInfo.QuotaLimit > 0 && keyInfo.QuotaPeriod != "" && keyInfo.QuotaPeriod != "none" {
		consumed, resetAt, err := p.traffic.GetQuotaUsage(keyInfo.ID, keyInfo.QuotaPeriod, keyInfo.ResetRule, time.Now())
		if err == nil && consumed >= keyInfo.QuotaLimit {
			slog.Warn("quota exceeded for api key", "key", keyInfo.Name, "consumed", consumed, "limit", keyInfo.QuotaLimit, "period", keyInfo.QuotaPeriod)
			writeQuotaExceeded(w, apiFormat, "", consumed, keyInfo.QuotaLimit, keyInfo.QuotaPeriod, keyInfo.ResetRule, resetAt)

			errMsg := fmt.Sprintf("quota exceeded (%d/%d tokens %s)", consumed, keyInfo.QuotaLimit, keyInfo.QuotaPeriod)
			_ = p.traffic.Record(&traffic.LogEntry{
				APIKey:       maskedKey,
				APIKeyName:   keyName,
				APIKeyID:     keyInfo.ID,
				Model:        "unknown",
				DurationMs:   int(time.Since(start).Milliseconds()),
				StatusCode:   http.StatusTooManyRequests,
				ClientIP:     clientIP,
				ErrorMessage: &errMsg,
				Level:        "WARN",
				ApiFormat:    apiFormat,
			})
			return
		}
	}

	// 3. Handle Other Requests (e.g. POST /v1/chat/completions)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, r, http.StatusBadRequest, "invalid_request_error", "bad_request", "failed to read request body", "")
		return
	}
	r.Body.Close()

	var req chatRequest
	_ = json.Unmarshal(bodyBytes, &req)
	modelName := strings.TrimSpace(req.Model)
	hasImages, imageCount := detectImages(req.Messages)
	thinkingRequested := DetectThinkingRequested(apiFormat, bodyBytes, modelName)

	// Intercept Chat Commands (e.g. /quota, /limits, /usage)
	isChatOrMessagesPath := (r.Method == http.MethodPost && (apiFormat == "chat" || apiFormat == "messages"))
	if isChatOrMessagesPath {
		chatCmdsEnabled := true
		if p.traffic != nil {
			chatCmdsEnabled = p.traffic.IsChatCommandsEnabled()
		}
		if chatCmdsEnabled {
			if cmd, arg, isCmd := IsChatCommand(req.Messages); isCmd {
				slog.Info("chat command intercepted", "command", cmd, "arg", arg, "key", keyName, "format", apiFormat)
				var markdown string
				if cmd == "/usage" {
					markdown = p.buildUsageMarkdown(r.Context(), keyInfo, arg)
				} else {
					quotaData := p.buildUnifiedQuotaResponse(r.Context(), keyInfo, false)
					markdown = FormatQuotaMarkdown(quotaData, arg)
				}
				if apiFormat == "messages" {
					WriteAnthropicChatCommandResponse(w, cmd, markdown, req.Stream)
				} else {
					WriteChatCommandResponse(w, cmd, markdown, req.Stream)
				}

				if p.traffic != nil {
					_ = p.traffic.Record(&traffic.LogEntry{
						APIKey:           maskedKey,
						APIKeyName:       keyName,
						APIKeyID:         keyInfo.ID,
						Model:            "nineguard/system",
						DurationMs:       int(time.Since(start).Milliseconds()),
						StatusCode:       http.StatusOK,
						TotalTokens:      0,
						PromptTokens:     0,
						CompletionTokens: 0,
						ClientIP:         clientIP,
						Stream:           req.Stream,
						Level:            "INFO",
						ApiFormat:        apiFormat,
					})
				}
				return
			}
		}
	}

	// Resolve the requested name to its canonical Model ID (ADR 0008) before any model check, so
	// "cx/gpt" and "9dak/cx/gpt" are one model for access, firewall and statistics. A resolve error
	// is reported later, at the same place as before.
	provider, actualModel, modelID, resolveErr := p.providers.ResolveModelID(modelName)

	// Check Allowed Models for this API Key (the requested name or its canonical form)
	if modelName != "" && keyInfo != nil && !keyInfo.IsModelAllowed(modelName) && !(modelID != "" && keyInfo.IsModelAllowed(modelID)) {
		slog.Warn("model forbidden for api key", "key", keyInfo.Name, "model", modelName, "ip", clientIP)
		writeProxyError(w, r, http.StatusForbidden, "permission_error", "model_not_allowed", fmt.Sprintf("Model '%s' is not allowed for API key '%s'.", modelName, keyInfo.Name), "model")

		errMsg := fmt.Sprintf("model '%s' is not allowed for API key '%s'", modelName, keyInfo.Name)
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:       maskedKey,
			APIKeyName:   keyName,
			APIKeyID:     keyInfo.ID,
			Model:        modelName,
			DurationMs:   int(time.Since(start).Milliseconds()),
			StatusCode:   http.StatusForbidden,
			ClientIP:     clientIP,
			Stream:       req.Stream,
			ErrorMessage: &errMsg,
			Level:        "ERROR",
			ApiFormat:    apiFormat,
		})
		return
	}

	// Unknown models are rejected, never registered: only the sync (or the admin) creates rows.
	// A provider with no registered models at all (it has no usable listing) is let through.
	var modelFound, modelEnabled bool
	if resolveErr == nil && modelID != "" {
		modelFound, modelEnabled, _ = p.models.Status(modelID)
		if !modelFound && p.models.HasModelsFor(provider.ID, provider.Prefix) {
			slog.Warn("unknown model rejected", "model", modelName, "resolved", modelID, "key", keyInfo.Name, "ip", clientIP)
			writeProxyError(w, r, http.StatusNotFound, "invalid_request_error", "model_not_found", fmt.Sprintf("The model '%s' does not exist. Use the full model id with its provider prefix (e.g. %s).", modelName, modelID), "model")

			errMsg := fmt.Sprintf("unknown model '%s'", modelName)
			_ = p.traffic.Record(&traffic.LogEntry{
				APIKey:       maskedKey,
				APIKeyName:   keyName,
				APIKeyID:     keyInfo.ID,
				ProviderID:   provider.ID,
				Model:        modelName,
				DurationMs:   int(time.Since(start).Milliseconds()),
				StatusCode:   http.StatusNotFound,
				ClientIP:     clientIP,
				Stream:       req.Stream,
				ErrorMessage: &errMsg,
				Level:        "WARN",
				ApiFormat:    apiFormat,
			})
			return
		}
	}

	// Check Model Blocking Rule in NineGuard (on the canonical ID)
	if modelFound && !modelEnabled {
		slog.Warn("model blocked by NineGuard firewall", "model", modelName, "resolved", modelID, "ip", clientIP)
		writeProxyError(w, r, http.StatusForbidden, "permission_error", "model_disabled", fmt.Sprintf("Model '%s' has been disabled by administrator.", modelName), "model")

		errMsg := "model disabled by NineGuard firewall"
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:       maskedKey,
			APIKeyName:   keyName,
			APIKeyID:     keyInfo.ID,
			Model:        modelName,
			DurationMs:   int(time.Since(start).Milliseconds()),
			StatusCode:   http.StatusForbidden,
			ClientIP:     clientIP,
			Stream:       req.Stream,
			ErrorMessage: &errMsg,
			Level:        "ERROR",
			ApiFormat:    apiFormat,
		})
		return
	}

	// Upstream provider was resolved above from the model prefix (e.g. openrouter/anthropic/claude-3.5-sonnet -> provider openrouter, model anthropic/claude-3.5-sonnet)
	if err := resolveErr; err != nil {
		writeProxyError(w, r, http.StatusBadGateway, "provider_error", "no_provider", fmt.Sprintf("%s. Configure an upstream provider in NineGuard (Providers menu).", err.Error()), "")
		return
	}

	// Model Quota (ADR 0007): checked on the resolved Model ID (ADR 0008), after the access and
	// disabled checks, so a bare "opus" counts against the quota of "9router/opus".
	if keyInfo.EnforcesModelQuotas() && modelID != "" && p.traffic != nil {
		if mq := keyInfo.ModelQuotaFor(modelID); mq != nil {
			consumed, resetAt, qerr := p.traffic.GetModelQuotaUsage(keyInfo.ID, modelID, mq.QuotaPeriod, keyInfo.ResetRule, time.Now())
			if qerr == nil && consumed >= mq.QuotaLimit {
				slog.Warn("model quota exceeded", "key", keyInfo.Name, "model", modelID, "consumed", consumed, "limit", mq.QuotaLimit, "period", mq.QuotaPeriod)
				writeQuotaExceeded(w, apiFormat, modelID, consumed, mq.QuotaLimit, mq.QuotaPeriod, keyInfo.ResetRule, resetAt)

				errMsg := fmt.Sprintf("model quota exceeded (%d/%d tokens %s) for %s", consumed, mq.QuotaLimit, mq.QuotaPeriod, modelID)
				_ = p.traffic.Record(&traffic.LogEntry{
					APIKey:       maskedKey,
					APIKeyName:   keyName,
					APIKeyID:     keyInfo.ID,
					ProviderID:   provider.ID,
					Model:        modelName,
					ModelID:      modelID,
					DurationMs:   int(time.Since(start).Milliseconds()),
					StatusCode:   http.StatusTooManyRequests,
					ClientIP:     clientIP,
					Stream:       req.Stream,
					ErrorMessage: &errMsg,
					Level:        "WARN",
					ApiFormat:    apiFormat,
				})
				return
			}
		}
	}

	// Rewrite request body if model ID changed (prefix stripped for upstream provider)
	forwardBody := bodyBytes
	if actualModel != modelName && actualModel != "" {
		var rawMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &rawMap); err == nil {
			rawMap["model"] = actualModel
			if rewritten, err := json.Marshal(rawMap); err == nil {
				forwardBody = rewritten
			}
		}
	}

	// 4. Intercept Chat Completions or Messages for Plugin Pipeline Execution
	isChat := (r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/chat/completions"))
	isChatOrMessages := (r.Method == http.MethodPost && (apiFormat == "chat" || apiFormat == "messages"))
	var pluginsApplied, pluginsSkipped, pluginErrors string
	var tokensSaved, tokensOverhead, pluginMs int
	var upstreamAppliedHeader string

	if isChatOrMessages && p.plugins != nil {
		isBypass := strings.EqualFold(r.Header.Get("X-NineGuard-Plugins"), "off")
		incomingApplied := r.Header.Get("X-NineGuard-Plugins-Applied")

		var groups []models.ModelGroup
		if p.models != nil {
			groups, _ = p.models.ListGroups()
		}

		pipeRes, appliedHdr, pipeErr := p.plugins.ExecutePipeline(
			r.Context(),
			groups,
			keyInfo.ID,
			keyName,
			actualModel,
			provider.ID,
			apiFormat,
			isBypass,
			incomingApplied,
			forwardBody,
		)
		if pipeErr != nil {
			slog.Error("plugin pipeline execution error", "error", pipeErr)
		} else if pipeRes != nil {
			pluginsApplied = strings.Join(pipeRes.PluginsApplied, ",")
			pluginsSkipped = strings.Join(pipeRes.PluginsSkipped, ",")
			pluginErrors = strings.Join(pipeRes.PluginErrors, ",")
			tokensSaved = pipeRes.TokensSaved
			tokensOverhead = pipeRes.TokensOverhead
			pluginMs = int(pipeRes.DurationMs)
			upstreamAppliedHeader = appliedHdr

			if pipeRes.Rejected {
				errType := "permission_error"
				errCode := "plugin_rejected"
				if pipeRes.RejectCode == http.StatusServiceUnavailable {
					errType = "server_error"
					errCode = "plugin_unavailable"
				}
				writeProxyError(w, r, pipeRes.RejectCode, errType, errCode, pipeRes.RejectMessage, "")

				errMsg := pipeRes.RejectMessage
				if p.traffic != nil {
					_ = p.traffic.Record(&traffic.LogEntry{
						APIKey:         maskedKey,
						APIKeyName:     keyName,
						APIKeyID:       keyInfo.ID,
						ProviderID:     provider.ID,
						Model:          modelName,
						ModelID:        modelID,
						DurationMs:     int(time.Since(start).Milliseconds()),
						StatusCode:     pipeRes.RejectCode,
						ClientIP:       clientIP,
						Stream:         req.Stream,
						ErrorMessage:   &errMsg,
						Level:          "ERROR",
						PluginsApplied: pluginsApplied,
						PluginsSkipped: pluginsSkipped,
						PluginErrors:   pluginErrors,
						PluginMs:       pluginMs,
						ApiFormat:      apiFormat,
					})
				}
				return
			}

			if len(pipeRes.Body) > 0 {
				forwardBody = pipeRes.Body
			}
		}
	}

	injectedUsage := false
	if isChat && req.Stream {
		streamInjEnabled := true
		if p.traffic != nil {
			streamInjEnabled = p.traffic.IsStreamUsageInjectionEnabled()
		}
		if streamInjEnabled {
			if updatedBody, injected, err := InjectStreamUsage(forwardBody); err == nil && injected {
				forwardBody = updatedBody
				injectedUsage = true
			}
		}
	}

	offeredTools := ExtractOfferedTools(apiFormat, forwardBody)
	if len(offeredTools) > 0 {
		forwardBody = CleanRequestToolHistory(apiFormat, forwardBody, offeredTools)
	}

	if apiFormat == "chat" && thinkingRequested {
		forwardBody = PreserveThinkingIntent(forwardBody)
	}

	// Build target URL
	targetBase := strings.TrimRight(provider.Route, "/")
	forwardPath := r.URL.Path
	if strings.HasSuffix(targetBase, "/v1") && strings.HasPrefix(forwardPath, "/v1") {
		forwardPath = strings.TrimPrefix(forwardPath, "/v1")
	}
	targetURL := targetBase + forwardPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(forwardBody))
	if err != nil {
		http.Error(w, `{"error":"failed to construct forward request"}`, http.StatusInternalServerError)
		return
	}

	for k, vv := range r.Header {
		for _, v := range vv {
			outReq.Header.Add(k, v)
		}
	}
	RemoveHopByHopHeaders(outReq.Header)
	outReq.Close = false
	outReq.Header.Del("X-NineGuard-Plugins") // Never forward bypass header upstream
	if upstreamAppliedHeader != "" {
		outReq.Header.Set("X-NineGuard-Plugins-Applied", upstreamAppliedHeader)
	} else {
		outReq.Header.Del("X-NineGuard-Plugins-Applied")
	}
	if parsedU, err := url.Parse(targetBase); err == nil {
		outReq.Host = parsedU.Host
	}
	if provider.APIKey != "" {
		outReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
		outReq.Header.Set("x-api-key", provider.APIKey)
	} else {
		outReq.Header.Del("Authorization")
		outReq.Header.Del("x-api-key")
	}
	if apiFormat == "messages" && outReq.Header.Get("anthropic-version") == "" {
		outReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Propagate recursion trace headers
	newTrace := p.instanceID
	if traceHeader != "" {
		if p.instanceID != "" {
			newTrace = traceHeader + "," + p.instanceID
		} else {
			newTrace = traceHeader
		}
	}
	if newTrace != "" {
		outReq.Header.Set("X-NineGuard-Trace", newTrace)
	}
	outReq.Header.Set("X-NineGuard-Hop", strconv.Itoa(hopCount+1))

	client := p.httpClient
	if client == nil {
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 5 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   15 * time.Minute,
		}
	}
	resp, err := client.Do(outReq)
	if err != nil {
		slog.Error("error forwarding to upstream provider", "provider", provider.ID, "error", err)
		writeProxyError(w, r, http.StatusBadGateway, "api_error", "bad_gateway", fmt.Sprintf("NineGuard could not reach upstream provider '%s': %v", provider.Name, err), "")

		errMsg := err.Error()
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:       maskedKey,
			APIKeyName:   keyName,
			APIKeyID:     keyInfo.ID,
			ProviderID:   provider.ID,
			Model:        modelName,
			ModelID:      modelID,
			DurationMs:   int(time.Since(start).Milliseconds()),
			StatusCode:   http.StatusBadGateway,
			ClientIP:     clientIP,
			Stream:       req.Stream,
			ErrorMessage: &errMsg,
			Level:        "ERROR",
			ApiFormat:    apiFormat,
		})
		return
	}
	defer resp.Body.Close()

	// A Removed Model that upstream rejects as unknown gets a clear 404 instead of the
	// provider's generic error (ADR 0006). Other statuses (429, 5xx...) pass through untouched.
	if (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest) &&
		p.models != nil && modelName != "" && p.models.IsModelRemoved(modelName) {
		upstreamBody, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("Model '%s' was removed from its provider and is no longer available.", modelName)
		rewritten, _ := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"message": msg,
				"type":    "invalid_request_error",
				"param":   "model",
				"code":    "model_removed",
			},
		})
		slog.Warn("upstream rejected a removed model", "model", modelName, "upstream_status", resp.StatusCode, "upstream_body", truncateForLog(upstreamBody))
		resp.StatusCode = http.StatusNotFound
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.Header.Set("Content-Type", "application/json")
		resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	}

	// Copy response headers
	RemoveHopByHopHeaders(resp.Header)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	if pluginsApplied != "" {
		w.Header().Set("X-NineGuard-Plugins-Applied", pluginsApplied)
	}
	if tokensSaved > 0 {
		w.Header().Set("X-NineGuard-Tokens-Saved", strconv.Itoa(tokensSaved))
	}
	w.Header().Add("Access-Control-Expose-Headers", "X-NineGuard-Plugins-Applied, X-NineGuard-Tokens-Saved")
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if !isSSE && len(offeredTools) > 0 {
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(resp.StatusCode)

	var promptTokens, completionTokens, totalTokens int
	var cacheReadTokens, cacheWriteTokens, reasoningTokens int
	var thinkingDelivered bool
	var servedModel string
	var usageEstimated bool
	var responseErrMsg *string
	var capturedRespBody []byte

	if isSSE {
		capturedRespBody = []byte("[Streaming SSE Event Stream]")
		flusher, isFlusher := w.(http.Flusher)
		reader := bufio.NewReader(resp.Body)
		chunkCount := 0
		var eventBuf bytes.Buffer
		diagParser := NewStreamDiagnosticsParser(apiFormat)

		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				eventBuf.Write(line)
				trimmed := bytes.TrimRight(line, "\r\n")
				if len(trimmed) == 0 {
					// Blank line completes an SSE event
					rawEvent := eventBuf.Bytes()
					isUsageOnly, dataPayload := IsUsageOnlySSEEvent(rawEvent)

					// Drop injected usage-only event from client stream (ADR 0011)
					if !(injectedUsage && isUsageOnly) {
						if len(offeredTools) > 0 {
							rawEvent = DecloakSSEEvent(apiFormat, rawEvent, dataPayload, offeredTools)
						}
						_, _ = w.Write(rawEvent)
						if isFlusher {
							flusher.Flush()
						}
					}

					diagParser.FeedSSEEvent(rawEvent, dataPayload)
					if len(dataPayload) > 0 && string(dataPayload) != "[DONE]" {
						chunkCount++
					}
					eventBuf.Reset()
				}
			}
			if err != nil {
				if eventBuf.Len() > 0 {
					rawEvent := eventBuf.Bytes()
					isUsageOnly, dataPayload := IsUsageOnlySSEEvent(rawEvent)
					if !(injectedUsage && isUsageOnly) {
						if len(offeredTools) > 0 {
							rawEvent = DecloakSSEEvent(apiFormat, rawEvent, dataPayload, offeredTools)
						}
						_, _ = w.Write(rawEvent)
						if isFlusher {
							flusher.Flush()
						}
					}
					diagParser.FeedSSEEvent(rawEvent, dataPayload)
				}
				break
			}
		}

		diag := diagParser.Finalize(chunkCount, len(bodyBytes), resp.StatusCode)
		promptTokens = diag.PromptTokens
		completionTokens = diag.CompletionTokens
		totalTokens = diag.TotalTokens
		cacheReadTokens = diag.CacheReadTokens
		cacheWriteTokens = diag.CacheWriteTokens
		reasoningTokens = diag.ReasoningTokens
		thinkingDelivered = diag.ThinkingDelivered
		servedModel = diag.ServedModel
		usageEstimated = diag.UsageEstimated
	} else {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			if len(offeredTools) > 0 {
				respBody = DecloakResponseBody(apiFormat, respBody, offeredTools)
			}
			capturedRespBody = respBody
			_, _ = w.Write(respBody)

			diag := ParseNonStreamDiagnostics(apiFormat, respBody)
			promptTokens = diag.PromptTokens
			completionTokens = diag.CompletionTokens
			totalTokens = diag.TotalTokens
			cacheReadTokens = diag.CacheReadTokens
			cacheWriteTokens = diag.CacheWriteTokens
			reasoningTokens = diag.ReasoningTokens
			thinkingDelivered = diag.ThinkingDelivered
			servedModel = diag.ServedModel
			if resp.StatusCode < 400 && totalTokens == 0 {
				usageEstimated = true
			}
			if diag.ErrorMessage != nil {
				responseErrMsg = diag.ErrorMessage
			}
		}
	}

	go func() {
		durMs := int(time.Since(start).Milliseconds())
		logEntry := &traffic.LogEntry{
			APIKey:            maskedKey,
			APIKeyName:        keyName,
			APIKeyID:          keyInfo.ID,
			ProviderID:        provider.ID,
			Model:             modelName,
			ModelID:           modelID,
			PromptTokens:      promptTokens,
			CompletionTokens:  completionTokens,
			TotalTokens:       totalTokens,
			DurationMs:        durMs,
			StatusCode:        resp.StatusCode,
			ClientIP:          clientIP,
			Stream:            isSSE || req.Stream,
			ErrorMessage:      responseErrMsg,
			PluginsApplied:    pluginsApplied,
			PluginsSkipped:    pluginsSkipped,
			TokensSaved:       tokensSaved,
			TokensOverhead:    tokensOverhead,
			PluginErrors:      pluginErrors,
			PluginMs:          pluginMs,
			HasImages:         hasImages,
			ImageCount:        imageCount,
			ApiFormat:         apiFormat,
			CacheReadTokens:   cacheReadTokens,
			CacheWriteTokens:  cacheWriteTokens,
			ReasoningTokens:   reasoningTokens,
			ThinkingRequested: thinkingRequested,
			ThinkingDelivered: thinkingDelivered,
			ServedModel:       servedModel,
			ModelFallback:     models.IsModelFallback(modelID, servedModel),
			UsageEstimated:    usageEstimated,
		}

		if p.pricingResolver != nil && !usageEstimated && totalTokens > 0 && modelID != "nineguard/system" {
			logEntry.CostUSD = p.pricingResolver.CalculateCost(modelID, promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens)
		}

		_ = p.traffic.Record(logEntry)

		if logEntry.ModelFallback && p.fallbackAlerter != nil && keyInfo != nil {
			p.fallbackAlerter.Alert(keyInfo.ID, keyName, modelID, servedModel, time.Now())
		}

		if p.traffic != nil {
			recMode := p.traffic.GetRecordPayloadsSetting()
			shouldRecord := (recMode == "all") || (recMode == "errors_only" && resp.StatusCode >= 400)
			if shouldRecord && logEntry.ID > 0 {
				_ = p.traffic.SavePayload(logEntry.ID, bodyBytes, capturedRespBody)
			}
		}

		if resp.StatusCode >= 400 {
			slog.Warn("proxy request failed", "model", modelName, "status", resp.StatusCode, "duration_ms", durMs, "source", "proxy", "plugins", pluginsApplied)
		} else {
			if pluginsApplied != "" {
				slog.Info("proxy request completed", "model", modelName, "status", resp.StatusCode, "duration_ms", durMs, "tokens", totalTokens, "plugins", pluginsApplied, "tokens_saved", tokensSaved, "source", "proxy")
			} else {
				slog.Info("proxy request completed", "model", modelName, "status", resp.StatusCode, "duration_ms", durMs, "tokens", totalTokens, "source", "proxy")
			}
		}

		heavyThreshold := 8000
		if p.traffic != nil {
			heavyThreshold = p.traffic.GetHeavyTokenThreshold()
		}
		if totalTokens >= heavyThreshold {
			slog.Warn("token_spike: heavy token usage detected", "source", "traffic", "key_id", keyInfo.ID, "key_name", keyInfo.Name, "tokens", totalTokens, "threshold", heavyThreshold)
		}
	}()
}

func truncateForLog(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

func formatTokenCount(n int64) string {
	in := strconv.FormatInt(n, 10)
	var out []byte
	l := len(in)
	for i := 0; i < l; i++ {
		if i > 0 && (l-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, in[i])
	}
	return string(out)
}

func (p *Proxy) buildUnifiedQuotaResponse(ctx context.Context, keyInfo *keys.KeyInfo, force bool) *upstream.UnifiedQuotaResponse {
	if p.upstream == nil {
		p.upstream = upstream.NewManager()
	}

	resp := &upstream.UnifiedQuotaResponse{
		UpstreamPools: make([]upstream.MergedPoolQuota, 0),
		ModelQuotas:   make([]upstream.ModelQuotaSummary, 0),
	}

	syncInterval := upstream.DefaultSyncInterval
	if p.traffic != nil {
		if d, err := upstream.ParseSyncInterval(p.traffic.GetUpstreamSyncInterval()); err == nil {
			syncInterval = d
		}
	}
	resp.SyncInterval = upstream.FormatInterval(syncInterval)

	now := time.Now()

	// 1. Client key budget (Key mode vs Model mode)
	if keyInfo != nil {
		if !keyInfo.EnforcesModelQuotas() {
			if keyInfo.QuotaLimit > 0 && keyInfo.QuotaPeriod != "" && keyInfo.QuotaPeriod != "none" && p.traffic != nil {
				consumed, resetAt, err := p.traffic.GetQuotaUsage(keyInfo.ID, keyInfo.QuotaPeriod, keyInfo.ResetRule, now)
				if err == nil {
					rem := keyInfo.QuotaLimit - consumed
					if rem < 0 {
						rem = 0
					}
					var resetTime *time.Time
					if !resetAt.IsZero() {
						resetTime = &resetAt
					}
					resp.KeyQuota = &upstream.ClientKeyBudget{
						Mode:      "key",
						Limit:     keyInfo.QuotaLimit,
						Used:      consumed,
						Remaining: rem,
						Period:    keyInfo.QuotaPeriod,
						ResetAt:   resetTime,
						Warning:   consumed >= int64(float64(keyInfo.QuotaLimit)*0.8),
					}
				}
			}
		} else {
			// Model mode
			if p.traffic != nil {
				for _, mq := range keyInfo.ModelQuotas {
					consumed, resetAt, err := p.traffic.GetModelQuotaUsage(keyInfo.ID, mq.ModelID, mq.QuotaPeriod, keyInfo.ResetRule, now)
					if err == nil {
						rem := mq.QuotaLimit - consumed
						if rem < 0 {
							rem = 0
						}
						var resetTime *time.Time
						if !resetAt.IsZero() {
							resetTime = &resetAt
						}
						resp.ModelQuotas = append(resp.ModelQuotas, upstream.ModelQuotaSummary{
							ModelID:   mq.ModelID,
							Limit:     mq.QuotaLimit,
							Used:      consumed,
							Remaining: rem,
							Period:    mq.QuotaPeriod,
							ResetAt:   resetTime,
							Warning:   consumed >= int64(float64(mq.QuotaLimit)*0.8),
						})
					}
				}
			}
		}
	}

	// 2. Upstream provider pools (served instantly from warm memory cache)
	if p.providers != nil {
		provList, err := p.providers.ListProviders()
		if err == nil {
			for _, prov := range provList {
				if !prov.IsActive || prov.QuotaType == "disabled" || prov.QuotaType == "" {
					continue
				}

				var res *upstream.ProviderQuotaResult
				if force {
					res, _ = p.upstream.GetProviderQuotas(ctx, prov, true)
				} else {
					res = p.upstream.GetCachedQuota(prov)
					if res == nil {
						// Cold start fallback: fetch with 3s timeout so request never hangs
						fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
						res, _ = p.upstream.GetProviderQuotas(fetchCtx, prov, false)
						cancel()
					}
				}

				if res != nil && len(res.Connections) > 0 {
					mergedPools := upstream.MergeAccountPools(prov.Name, res.Connections)
					resp.UpstreamPools = append(resp.UpstreamPools, mergedPools...)
					if resp.LastUpdated == nil || res.LastUpdated.After(*resp.LastUpdated) {
						tCopy := res.LastUpdated
						resp.LastUpdated = &tCopy
					}
				}
			}
		}
	}

	// 3. Filter pools if key has restricted model access
	if keyInfo != nil && !keyInfo.IsAllModelsAllowed() {
		resp.UpstreamPools = upstream.FilterPoolsByModelAccess(resp.UpstreamPools, keyInfo.AllowedModels)
	}

	return resp
}

func (p *Proxy) buildUsageSummaryResponse(ctx context.Context, keyInfo *keys.KeyInfo, rng string) (*traffic.UsageSummary, error) {
	if p.traffic == nil {
		return nil, fmt.Errorf("traffic manager not initialized")
	}

	loc := time.UTC
	if keyInfo != nil && keyInfo.EffectiveResetSchedule != nil {
		loc = keyInfo.EffectiveResetSchedule.Location()
	}

	keyID := ""
	if keyInfo != nil {
		keyID = keyInfo.ID
	}

	summary, err := p.traffic.GetUsageSummary(keyID, rng, loc, time.Now(), p.pricingManager)
	if err != nil {
		return nil, err
	}

	globalCostVisible := true
	if p.pricingManager != nil {
		if cs, err := p.pricingManager.GetCostSettings(); err == nil {
			globalCostVisible = cs.CostVisibleToKeys
		}
	}

	var keyCostVis *string
	if keyInfo != nil {
		keyCostVis = keyInfo.CostVisibility
	}
	showCost := pricing.ShowCost(keyCostVis, globalCostVisible)

	if !showCost {
		summary.Cost = nil
		summary.UpstreamUsage = nil
	} else if p.upstream != nil && p.providers != nil {
		provList, _ := p.providers.ListProviders()
		for _, prov := range provList {
			if prov.IsActive && prov.QuotaType == "nineguard" {
				upSummary, upErr := p.upstream.FetchUpstreamUsage(ctx, prov.ID, prov.Route, prov.APIKey, rng)
				entry := traffic.UpstreamUsageEntry{
					Provider: prov.Name,
					Summary:  upSummary,
				}
				if upErr != nil {
					entry.Error = upErr.Error()
				}
				summary.UpstreamUsage = append(summary.UpstreamUsage, entry)
			}
		}
	}

	return summary, nil
}

func (p *Proxy) buildUsageMarkdown(ctx context.Context, keyInfo *keys.KeyInfo, arg string) string {
	rng := "30d"
	argClean := strings.ToLower(strings.TrimSpace(arg))
	if argClean == "7d" || argClean == "all" {
		rng = argClean
	}

	summary, err := p.buildUsageSummaryResponse(ctx, keyInfo, rng)
	if err != nil {
		return fmt.Sprintf("Error generating usage summary: %v", err)
	}

	showCost := (summary.Cost != nil)
	return FormatUsageMarkdown(summary, arg, showCost)
}

func (p *Proxy) handleGetUsage(w http.ResponseWriter, r *http.Request, keyInfo *keys.KeyInfo, maskedKey, keyName, clientIP string, start time.Time) {
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "30d"
	}
	rng = strings.ToLower(strings.TrimSpace(rng))
	if rng != "7d" && rng != "30d" && rng != "all" {
		http.Error(w, `{"error":{"message":"Invalid range parameter, must be 7d, 30d, or all","type":"invalid_request_error"}}`, http.StatusBadRequest)
		return
	}

	summary, err := p.buildUsageSummaryResponse(r.Context(), keyInfo, rng)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":%q,"type":"internal_server_error"}}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)

	if p.traffic != nil {
		keyID := ""
		if keyInfo != nil {
			keyID = keyInfo.ID
		}
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:       maskedKey,
			APIKeyName:   keyName,
			APIKeyID:     keyID,
			Model:        "nineguard/system",
			DurationMs:   int(time.Since(start).Milliseconds()),
			StatusCode:   http.StatusOK,
			ClientIP:     clientIP,
			Stream:       false,
			Level:        "INFO",
		})
	}
}

