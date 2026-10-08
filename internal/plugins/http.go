package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type HTTPPluginContext struct {
	APIKeyName string         `json:"api_key_name"`
	Model      string         `json:"model"`
	Provider   string         `json:"provider"`
	Groups     []string       `json:"groups"`
	Settings   map[string]any `json:"settings"`
}

type HTTPPluginResponse struct {
	Request        map[string]any `json:"request,omitempty"`
	Action         string         `json:"action,omitempty"`
	Message        string         `json:"message,omitempty"`
	TokensSaved    int            `json:"tokens_saved,omitempty"`
	TokensOverhead int            `json:"tokens_overhead,omitempty"`
}

// CallHTTPPlugin sends the chat completion request and context to a third-party HTTP plugin.
func CallHTTPPlugin(
	ctx context.Context,
	client *http.Client,
	p Plugin,
	body map[string]any,
	pluginCtx HTTPPluginContext,
) (modifiedBody map[string]any, tokensSaved, tokensOverhead int, rejected bool, rejectMessage string, err error) {
	if p.URL == "" {
		return body, 0, 0, false, "", fmt.Errorf("plugin %s has empty url", p.ID)
	}

	timeoutMs := p.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	payload := map[string]any{
		"request": body,
		"context": pluginCtx,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return body, 0, 0, false, "", fmt.Errorf("failed to marshal http plugin payload: %w", err)
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.URL, bytes.NewReader(b))
	if err != nil {
		return body, 0, 0, false, "", fmt.Errorf("failed to create http plugin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Secret != "" {
		req.Header.Set("X-NineGuard-Plugin-Secret", p.Secret)
	}

	httpClient := client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return body, 0, 0, false, "", fmt.Errorf("http plugin request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, 0, 0, false, "", fmt.Errorf("http plugin returned status %d", resp.StatusCode)
	}

	var res HTTPPluginResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return body, 0, 0, false, "", fmt.Errorf("failed to decode http plugin response: %w", err)
	}

	if strings.EqualFold(res.Action, "reject") {
		msg := res.Message
		if msg == "" {
			msg = fmt.Sprintf("Request rejected by plugin %s", p.Name)
		}
		return body, 0, 0, true, msg, nil
	}

	if res.Request == nil {
		return body, 0, 0, false, "", fmt.Errorf("http plugin returned missing request body")
	}

	// Model preservation rule: if model changed, restore original and log warning
	originalModel, _ := body["model"].(string)
	returnedModel, _ := res.Request["model"].(string)
	if originalModel != "" && returnedModel != originalModel {
		slog.Warn("plugin attempted to modify model; restoring original", "plugin", p.ID, "original", originalModel, "modified", returnedModel)
		res.Request["model"] = originalModel
	}

	return res.Request, res.TokensSaved, res.TokensOverhead, false, "", nil
}
