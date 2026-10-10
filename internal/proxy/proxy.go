package proxy

import (
	"bufio"
	"bytes"
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
	"nineguard/internal/providers"
	"nineguard/internal/traffic"
)

type Proxy struct {
	models     *models.Manager
	traffic    *traffic.Manager
	keys       *keys.Manager
	providers  *providers.Manager
	plugins    *plugins.Manager
	httpClient *http.Client
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
		plugins:    plm,
		httpClient: client,
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

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		clientIP = strings.Split(forwarded, ",")[0]
	}

	// 1. Strict Gatekeeper: Validate Client NineGuard API Key
	clientAuth := r.Header.Get("Authorization")
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
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		errJSON := `{
			"error": {
				"message": "Invalid or inactive NineGuard API key. Please generate an API key in the NineGuard dashboard (Endpoints & Keys).",
				"type": "authentication_error",
				"code": "invalid_api_key"
			}
		}`
		_, _ = w.Write([]byte(errJSON))
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

	// 2.5 Enforce Token Quotas per API Key (ADR 0005)
	if p.traffic != nil && keyInfo != nil && keyInfo.QuotaLimit > 0 && keyInfo.QuotaPeriod != "" && keyInfo.QuotaPeriod != "none" {
		consumed, resetAt, err := p.traffic.GetQuotaUsage(keyInfo.ID, keyInfo.QuotaPeriod, time.Now())
		if err == nil && consumed >= keyInfo.QuotaLimit {
			slog.Warn("quota exceeded for api key", "key", keyInfo.Name, "consumed", consumed, "limit", keyInfo.QuotaLimit, "period", keyInfo.QuotaPeriod)
			retryAfterSecs := int(time.Until(resetAt).Seconds())
			if retryAfterSecs < 1 {
				retryAfterSecs = 1
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSecs))
			w.WriteHeader(http.StatusTooManyRequests)

			durationUntil := time.Until(resetAt)
			hours := int(durationUntil.Hours())
			mins := int(durationUntil.Minutes()) % 60
			resetDesc := fmt.Sprintf("%dh %dm", hours, mins)
			if hours <= 0 {
				resetDesc = fmt.Sprintf("%dm", mins)
			}

			wibLoc, err := time.LoadLocation("Asia/Jakarta")
			if err != nil {
				wibLoc = time.FixedZone("WIB", 7*3600)
			}
			wibTime := resetAt.In(wibLoc).Format("15:04 WIB")
			utcTime := resetAt.UTC().Format("15:04 UTC")
			var atDisplay string
			if keyInfo.QuotaPeriod == "weekly" || keyInfo.QuotaPeriod == "monthly" {
				wibDate := resetAt.In(wibLoc).Format("02 Jan")
				atDisplay = fmt.Sprintf("%s on %s / %s", wibTime, wibDate, utcTime)
			} else {
				atDisplay = fmt.Sprintf("%s / %s", wibTime, utcTime)
			}

			errJSON := fmt.Sprintf(`{
	"error": {
		"message": "API key token quota exceeded (%s / %s tokens %s). Resets in %s (at %s).",
		"type": "insufficient_quota",
		"code": "quota_exceeded"
	}
}`, formatTokenCount(consumed), formatTokenCount(keyInfo.QuotaLimit), keyInfo.QuotaPeriod, resetDesc, atDisplay)
			_, _ = w.Write([]byte(errJSON))

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
			})
			return
		}
	}

	// 3. Handle Other Requests (e.g. POST /v1/chat/completions)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	r.Body.Close()

	var req chatRequest
	_ = json.Unmarshal(bodyBytes, &req)
	modelName := strings.TrimSpace(req.Model)
	hasImages, imageCount := detectImages(req.Messages)

	// Check Allowed Models for this API Key
	if modelName != "" && keyInfo != nil && !keyInfo.IsModelAllowed(modelName) {
		slog.Warn("model forbidden for api key", "key", keyInfo.Name, "model", modelName, "ip", clientIP)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		errJSON := fmt.Sprintf(`{
			"error": {
				"message": "Model '%s' is not allowed for API key '%s'.",
				"type": "permission_error",
				"param": "model",
				"code": "model_not_allowed"
			}
		}`, modelName, keyInfo.Name)
		_, _ = w.Write([]byte(errJSON))

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
		})
		return
	}

	// Check Model Blocking Rule in NineGuard
	if modelName != "" && !p.models.IsModelEnabled(modelName) {
		slog.Warn("model blocked by NineGuard firewall", "model", modelName, "ip", clientIP)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		errJSON := fmt.Sprintf(`{
			"error": {
				"message": "Model '%s' has been disabled by administrator.",
				"type": "permission_error",
				"param": "model",
				"code": "model_disabled"
			}
		}`, modelName)
		_, _ = w.Write([]byte(errJSON))

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
		})
		return
	}

	// Resolve Upstream Provider based on model prefix (e.g. openrouter/anthropic/claude-3.5-sonnet -> provider openrouter, model anthropic/claude-3.5-sonnet)
	provider, actualModel, err := p.providers.FindProviderForModel(modelName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		errJSON := fmt.Sprintf(`{
			"error": {
				"message": "%s. Configure an upstream provider in NineGuard (Providers menu).",
				"type": "provider_error",
				"code": "no_provider"
			}
		}`, err.Error())
		_, _ = w.Write([]byte(errJSON))
		return
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

	// 4. Intercept Chat Completions for Plugin Pipeline Execution
	isChat := (r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/chat/completions"))
	var pluginsApplied, pluginsSkipped, pluginErrors string
	var tokensSaved, tokensOverhead, pluginMs int
	var upstreamAppliedHeader string

	if isChat && p.plugins != nil {
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(pipeRes.RejectCode)
				errType := "permission_error"
				errCode := "plugin_rejected"
				if pipeRes.RejectCode == http.StatusServiceUnavailable {
					errType = "server_error"
					errCode = "plugin_unavailable"
				}
				errJSON := fmt.Sprintf(`{"error":{"message":%q,"type":%q,"param":null,"code":%q}}`, pipeRes.RejectMessage, errType, errCode)
				_, _ = w.Write([]byte(errJSON))

				errMsg := pipeRes.RejectMessage
				if p.traffic != nil {
					_ = p.traffic.Record(&traffic.LogEntry{
						APIKey:         maskedKey,
						APIKeyName:     keyName,
						APIKeyID:       keyInfo.ID,
						ProviderID:     provider.ID,
						Model:          modelName,
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
					})
				}
				return
			}

			if len(pipeRes.Body) > 0 {
				forwardBody = pipeRes.Body
			}
		}
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
	} else {
		outReq.Header.Del("Authorization")
	}

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":"NineGuard could not reach upstream provider '%s': %v"}}`, provider.Name, err)))

		errMsg := err.Error()
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:       maskedKey,
			APIKeyName:   keyName,
			APIKeyID:     keyInfo.ID,
			ProviderID:   provider.ID,
			Model:        modelName,
			DurationMs:   int(time.Since(start).Milliseconds()),
			StatusCode:   http.StatusBadGateway,
			ClientIP:     clientIP,
			Stream:       req.Stream,
			ErrorMessage: &errMsg,
			Level:        "ERROR",
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
	w.WriteHeader(resp.StatusCode)

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	var promptTokens, completionTokens, totalTokens int
	var responseErrMsg *string
	var capturedRespBody []byte

	if isSSE {
		capturedRespBody = []byte("[Streaming SSE Event Stream]")
		flusher, isFlusher := w.(http.Flusher)
		reader := bufio.NewReader(resp.Body)
		chunkCount := 0

		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = w.Write(line)
				if isFlusher {
					flusher.Flush()
				}
				lineStr := string(line)
				if strings.HasPrefix(lineStr, "data: ") {
					dataPayload := strings.TrimSpace(lineStr[6:])
					if dataPayload != "[DONE]" && strings.Contains(dataPayload, `"usage"`) {
						var chunkData struct {
							Usage openAIUsage `json:"usage"`
						}
						if err := json.Unmarshal([]byte(dataPayload), &chunkData); err == nil && chunkData.Usage.TotalTokens > 0 {
							promptTokens = chunkData.Usage.PromptTokens
							completionTokens = chunkData.Usage.CompletionTokens
							totalTokens = chunkData.Usage.TotalTokens
						}
					}
					chunkCount++
				}
			}
			if err != nil {
				break
			}
		}

		if totalTokens == 0 && chunkCount > 0 {
			completionTokens = chunkCount
			if completionTokens < 1 {
				completionTokens = 1
			}
			promptTokens = len(bodyBytes) / 4
			totalTokens = promptTokens + completionTokens
		}
	} else {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			capturedRespBody = respBody
			_, _ = w.Write(respBody)

			var chatResp chatResponse
			if err := json.Unmarshal(respBody, &chatResp); err == nil {
				promptTokens = chatResp.Usage.PromptTokens
				completionTokens = chatResp.Usage.CompletionTokens
				totalTokens = chatResp.Usage.TotalTokens
				if chatResp.Error != nil && chatResp.Error.Message != "" {
					responseErrMsg = &chatResp.Error.Message
				}
			}
		}
	}

	go func() {
		durMs := int(time.Since(start).Milliseconds())
		logEntry := &traffic.LogEntry{
			APIKey:           maskedKey,
			APIKeyName:       keyName,
			APIKeyID:         keyInfo.ID,
			ProviderID:       provider.ID,
			Model:            modelName,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			DurationMs:       durMs,
			StatusCode:       resp.StatusCode,
			ClientIP:         clientIP,
			Stream:           isSSE || req.Stream,
			ErrorMessage:     responseErrMsg,
			PluginsApplied:   pluginsApplied,
			PluginsSkipped:   pluginsSkipped,
			TokensSaved:      tokensSaved,
			TokensOverhead:   tokensOverhead,
			PluginErrors:     pluginErrors,
			PluginMs:         pluginMs,
			HasImages:        hasImages,
			ImageCount:       imageCount,
		}
		_ = p.traffic.Record(logEntry)

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
