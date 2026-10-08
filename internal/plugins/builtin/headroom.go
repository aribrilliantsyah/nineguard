package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ApplyHeadroom calls Headroom compressor endpoint to compress messages in reqBody.
func ApplyHeadroom(ctx context.Context, client *http.Client, reqBody map[string]any, settings map[string]any) (modifiedBody map[string]any, tokensSaved int, skipped bool, skipReason string, err error) {
	url := "http://127.0.0.1:8787"
	mode := "incremental"
	compressUserMessages := false
	token := ""

	if settings != nil {
		if u, ok := settings["url"].(string); ok && strings.TrimSpace(u) != "" {
			url = strings.TrimRight(strings.TrimSpace(u), "/")
		}
		if m, ok := settings["mode"].(string); ok && strings.TrimSpace(m) != "" {
			mode = strings.ToLower(strings.TrimSpace(m))
		}
		if c, ok := settings["compress_user_messages"].(bool); ok {
			compressUserMessages = c
		}
		if t, ok := settings["token"].(string); ok && strings.TrimSpace(t) != "" {
			token = strings.TrimSpace(t)
		}
	}

	rawMsgs, _ := reqBody["messages"].([]any)
	if len(rawMsgs) == 0 {
		return reqBody, 0, false, "", nil
	}

	// Compute frozen message count
	frozenCount := 0
	if mode == "incremental" {
		lastAssistantIdx := -1
		for i, m := range rawMsgs {
			if mMap, ok := m.(map[string]any); ok {
				if role, _ := mMap["role"].(string); role == "assistant" {
					lastAssistantIdx = i
				}
			}
		}
		if lastAssistantIdx >= 0 {
			frozenCount = lastAssistantIdx + 1
		}
	}

	model, _ := reqBody["model"].(string)

	payload := map[string]any{
		"messages": rawMsgs,
		"model":    model,
		"config": map[string]any{
			"frozen_message_count":   frozenCount,
			"compress_user_messages": compressUserMessages,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return reqBody, 0, false, "", fmt.Errorf("headroom marshal request error: %w", err)
	}

	endpoint := url + "/v1/compress"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return reqBody, 0, false, "", fmt.Errorf("headroom new request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	httpClient := client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return reqBody, 0, false, "", fmt.Errorf("headroom http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return reqBody, 0, false, "", fmt.Errorf("headroom status %d", resp.StatusCode)
	}

	var headroomResp struct {
		Messages           []any `json:"messages"`
		TokensSaved        int   `json:"tokens_saved"`
		CompressionSkipped bool  `json:"compression_skipped"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&headroomResp); err != nil {
		return reqBody, 0, false, "", fmt.Errorf("headroom response decode error: %w", err)
	}

	if headroomResp.CompressionSkipped {
		return reqBody, 0, false, "", nil
	}

	// Clone original body and substitute messages
	cloned := make(map[string]any, len(reqBody))
	for k, v := range reqBody {
		cloned[k] = v
	}

	if len(headroomResp.Messages) > 0 {
		cloned["messages"] = headroomResp.Messages
	}

	return cloned, headroomResp.TokensSaved, false, "", nil
}
