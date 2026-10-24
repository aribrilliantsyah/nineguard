package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestChatCommand_Interception(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_cmd_interception.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	mm := models.NewManager(d)
	pm := providers.NewManager(d)

	k, err := km.CreateKeyWithOptions("Cmd Key", keys.CreateKeyOptions{
		QuotaLimit:  500000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// 1. Test non-streaming /quota
	bodyJSON := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"/quota"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	content := resp.Choices[0].Message.Content
	if !strings.Contains(content, "NineGuard Quota Report") {
		t.Fatalf("expected quota report markdown, got: %s", content)
	}
	if !strings.Contains(content, "500,000 tokens remaining") {
		t.Fatalf("expected 500,000 tokens in report, got: %s", content)
	}
	if resp.Usage.TotalTokens != 0 {
		t.Fatalf("expected 0 total tokens, got %d", resp.Usage.TotalTokens)
	}

	// 2. Test streaming /limits
	streamBodyJSON := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"/limits"}]}`
	streamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBodyJSON))
	streamReq.Header.Set("Authorization", "Bearer "+k.RawKey)
	streamRec := httptest.NewRecorder()

	p.ServeHTTP(streamRec, streamReq)

	if streamRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", streamRec.Code, streamRec.Body.String())
	}
	streamOutput := streamRec.Body.String()
	if !strings.Contains(streamOutput, "data: [DONE]") {
		t.Fatalf("expected SSE stream with [DONE], got: %s", streamOutput)
	}
	if !strings.Contains(streamOutput, "NineGuard Quota Report") {
		t.Fatalf("expected markdown in stream chunk, got: %s", streamOutput)
	}

	// 2.5 Test content parts array (Pi/Monocode format) and arguments: /quota key
	partsBodyJSON := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"/quota key"}]}]}`
	partsReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(partsBodyJSON))
	partsReq.Header.Set("Authorization", "Bearer "+k.RawKey)
	partsRec := httptest.NewRecorder()

	p.ServeHTTP(partsRec, partsReq)

	if partsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for content parts, got %d: %s", partsRec.Code, partsRec.Body.String())
	}
	var partsResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(partsRec.Body.Bytes(), &partsResp)
	partsContent := partsResp.Choices[0].Message.Content
	if !strings.Contains(partsContent, "Key Allowance (Admin Limit)") {
		t.Fatalf("expected Key Allowance in /quota key output, got: %s", partsContent)
	}
	if strings.Contains(partsContent, "Upstream Provider Capacity") {
		t.Fatalf("expected Upstream Provider Capacity to be omitted when argument is 'key', got: %s", partsContent)
	}

	// 3. Test toggle: disable chat commands
	_ = tm.SetChatCommandsEnabled(false)

	// A normal request when disabled should proceed to upstream routing rather than intercepting
	recDisabled := httptest.NewRecorder()
	p.ServeHTTP(recDisabled, req)
	// Without upstream provider configured for gpt-4o, it fails at model routing, not intercepted
	if strings.Contains(recDisabled.Body.String(), "NineGuard Quota Report") {
		t.Fatalf("expected chat command to be bypassed when disabled, but it was intercepted")
	}
}

func TestChatCommand_AnthropicMessages(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_cmd_anthropic.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	mm := models.NewManager(d)
	pm := providers.NewManager(d)

	k, err := km.CreateKeyWithOptions("Claude Code Key", keys.CreateKeyOptions{
		QuotaLimit:  1000000,
		QuotaPeriod: "weekly",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// 1. Non-streaming /quota on /v1/messages
	bodyJSON := `{"model":"claude-3-7-sonnet","stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"/quota"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(bodyJSON))
	req.Header.Set("x-api-key", k.RawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal anthropic message response: %v, body=%s", err, rec.Body.String())
	}
	if resp.Type != "message" || resp.Role != "assistant" || resp.StopReason != "end_turn" {
		t.Errorf("unexpected anthropic response metadata: %+v", resp)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" {
		t.Fatalf("expected 1 text content block, got %+v", resp.Content)
	}
	if !strings.Contains(resp.Content[0].Text, "NineGuard Quota Report") {
		t.Errorf("expected Quota Report in content, got: %s", resp.Content[0].Text)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("expected 0 usage tokens, got %+v", resp.Usage)
	}

	// 2. Streaming /quota on /v1/messages
	streamBodyJSON := `{"model":"claude-3-7-sonnet","stream":true,"messages":[{"role":"user","content":"/quota"}]}`
	streamReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(streamBodyJSON))
	streamReq.Header.Set("x-api-key", k.RawKey)
	streamReq.Header.Set("Content-Type", "application/json")
	streamRec := httptest.NewRecorder()

	p.ServeHTTP(streamRec, streamReq)

	if streamRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", streamRec.Code, streamRec.Body.String())
	}
	outSSE := streamRec.Body.String()
	if !strings.Contains(outSSE, "event: message_start") {
		t.Errorf("missing message_start event: %s", outSSE)
	}
	if !strings.Contains(outSSE, "event: content_block_delta") {
		t.Errorf("missing content_block_delta event: %s", outSSE)
	}
	if !strings.Contains(outSSE, "event: message_stop") {
		t.Errorf("missing message_stop event: %s", outSSE)
	}
	if !strings.Contains(outSSE, "NineGuard Quota Report") {
		t.Errorf("missing quota report text in delta: %s", outSSE)
	}
}
