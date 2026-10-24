package proxy_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyAnthropicMessagesIntegration(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	var receivedAuth, receivedXAPIKey, receivedAnthropicVersion, receivedAnthropicBeta string

	// Mock upstream handles /v1/messages streaming
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedXAPIKey = r.Header.Get("x-api-key")
		receivedAnthropicVersion = r.Header.Get("anthropic-version")
		receivedAnthropicBeta = r.Header.Get("anthropic-beta")

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Event 1: message_start
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-3-7-sonnet-20250219\",\"usage\":{\"input_tokens\":500,\"cache_read_input_tokens\":2000,\"cache_creation_input_tokens\":300}}}\n\n"))
		flusher.Flush()

		// Event 2: content_block_start with thinking
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"))
		flusher.Flush()

		// Event 3: message_delta with output tokens
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":600}}\n\n"))
		flusher.Flush()

		// Event 4: message_stop
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	_, _ = providersMgr.CreateProvider("test-anthropic", upstream.URL, "sk-test", "", true, true)
	clientKey, _ := keyMgr.CreateKey("claude-code-agent", "all", nil, nil)
	p, _ := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)

	reqBody := `{"model":"claude-3-7-sonnet","stream":true,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":"build a feature"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if receivedAuth != "Bearer sk-test" {
		t.Errorf("upstream Authorization = %q, expected 'Bearer sk-test'", receivedAuth)
	}
	if receivedXAPIKey != "sk-test" {
		t.Errorf("upstream x-api-key = %q, expected 'sk-test'", receivedXAPIKey)
	}
	if receivedAnthropicVersion != "2023-06-01" {
		t.Errorf("upstream anthropic-version = %q, expected '2023-06-01'", receivedAnthropicVersion)
	}
	if receivedAnthropicBeta != "prompt-caching-2024-07-31" {
		t.Errorf("upstream anthropic-beta = %q, expected 'prompt-caching-2024-07-31'", receivedAnthropicBeta)
	}

	// Wait briefly for asynchronous traffic logging goroutine
	time.Sleep(50 * time.Millisecond)

	logs, total, err := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got total=%d len=%d", total, len(logs))
	}

	entry := logs[0]
	if entry.ApiFormat != "messages" {
		t.Errorf("ApiFormat = %q; expected 'messages'", entry.ApiFormat)
	}
	// Prompt = 500 + 2000 + 300 = 2800
	if entry.PromptTokens != 2800 {
		t.Errorf("PromptTokens = %d; expected 2800", entry.PromptTokens)
	}
	if entry.CompletionTokens != 600 {
		t.Errorf("CompletionTokens = %d; expected 600", entry.CompletionTokens)
	}
	if entry.TotalTokens != 3400 {
		t.Errorf("TotalTokens = %d; expected 3400", entry.TotalTokens)
	}
	if entry.CacheReadTokens != 2000 {
		t.Errorf("CacheReadTokens = %d; expected 2000", entry.CacheReadTokens)
	}
	if entry.CacheWriteTokens != 300 {
		t.Errorf("CacheWriteTokens = %d; expected 300", entry.CacheWriteTokens)
	}
	if !entry.ThinkingRequested {
		t.Errorf("expected ThinkingRequested=true")
	}
	if !entry.ThinkingDelivered {
		t.Errorf("expected ThinkingDelivered=true")
	}
	if entry.ServedModel != "claude-3-7-sonnet-20250219" {
		t.Errorf("ServedModel = %q; expected 'claude-3-7-sonnet-20250219'", entry.ServedModel)
	}
	if entry.ModelFallback {
		t.Errorf("ModelFallback = true; expected false (same family)")
	}
	if entry.UsageEstimated {
		t.Errorf("UsageEstimated = true; expected false")
	}
}
