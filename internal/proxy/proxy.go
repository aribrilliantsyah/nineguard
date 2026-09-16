package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/traffic"
)

type Proxy struct {
	models    *models.Manager
	traffic   *traffic.Manager
	keys      *keys.Manager
	providers *providers.Manager
}

func NewProxy(mm *models.Manager, tm *traffic.Manager, km *keys.Manager, pm *providers.Manager) (*Proxy, error) {
	return &Proxy{
		models:    mm,
		traffic:   tm,
		keys:      km,
		providers: pm,
	}, nil
}

type chatRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
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
	if parsedU, err := url.Parse(targetBase); err == nil {
		outReq.Host = parsedU.Host
	}
	if provider.APIKey != "" {
		outReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	} else {
		outReq.Header.Del("Authorization")
	}

	client := &http.Client{
		Timeout: 0, // LLM generation can take minutes
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

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	var promptTokens, completionTokens, totalTokens int
	var responseErrMsg *string

	if isSSE {
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
		_ = p.traffic.Record(&traffic.LogEntry{
			APIKey:           maskedKey,
			APIKeyName:       keyName,
			ProviderID:       provider.ID,
			Model:            modelName,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			DurationMs:       int(time.Since(start).Milliseconds()),
			StatusCode:       resp.StatusCode,
			ClientIP:         clientIP,
			Stream:           isSSE || req.Stream,
			ErrorMessage:     responseErrMsg,
		})
	}()
}
