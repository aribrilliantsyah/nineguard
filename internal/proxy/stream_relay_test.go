package proxy_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestIsUsageOnlySSEEvent(t *testing.T) {
	t.Run("usage-only chunk with empty choices", func(t *testing.T) {
		event := "data: {\"id\":\"chatcmpl-123\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"
		isUsageOnly, data := proxy.IsUsageOnlySSEEvent([]byte(event))
		if !isUsageOnly {
			t.Errorf("expected isUsageOnly=true")
		}
		if len(data) == 0 {
			t.Errorf("expected data to be extracted")
		}
	})

	t.Run("chunk with content choices and usage", func(t *testing.T) {
		event := "data: {\"id\":\"chatcmpl-123\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}\n\n"
		isUsageOnly, _ := proxy.IsUsageOnlySSEEvent([]byte(event))
		if isUsageOnly {
			t.Errorf("expected isUsageOnly=false when choices has items")
		}
	})

	t.Run("standard delta chunk without usage", func(t *testing.T) {
		event := "data: {\"id\":\"chatcmpl-123\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
		isUsageOnly, _ := proxy.IsUsageOnlySSEEvent([]byte(event))
		if isUsageOnly {
			t.Errorf("expected isUsageOnly=false")
		}
	})

	t.Run("done chunk", func(t *testing.T) {
		event := "data: [DONE]\n\n"
		isUsageOnly, data := proxy.IsUsageOnlySSEEvent([]byte(event))
		if isUsageOnly {
			t.Errorf("expected isUsageOnly=false for [DONE]")
		}
		if string(data) != "[DONE]" {
			t.Errorf("expected data=[DONE], got %s", string(data))
		}
	})
}

func TestStreamUsageInjectionAndStripping(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	// Upstream verifies stream_options was injected and emits usage-only chunk at end
	upstreamReceivedInjected := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)
		if strings.Contains(string(reqBody), `"include_usage":true`) {
			upstreamReceivedInjected = true
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Event 1: content chunk
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		flusher.Flush()

		// Event 2: usage-only chunk
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\n"))
		flusher.Flush()

		// Event 3: [DONE]
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	_, _ = providersMgr.CreateProvider("test-up", upstream.URL, "sk-test", "", true, true)
	clientKey, _ := keyMgr.CreateKey("stream-agent", "all", nil, nil)
	p, _ := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)

	// Client sends request WITHOUT stream_options.include_usage
	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if !upstreamReceivedInjected {
		t.Errorf("expected upstream to receive injected include_usage: true")
	}

	clientResp := rec.Body.String()
	// Client response MUST contain the content event
	if !strings.Contains(clientResp, `"content":"Hello"`) {
		t.Errorf("expected client response to contain content event")
	}
	// Client response MUST NOT contain the usage-only chunk because client never asked for it!
	if strings.Contains(clientResp, `"total_tokens":120`) {
		t.Errorf("usage-only chunk was not stripped from client response: %s", clientResp)
	}
	// Client response MUST contain [DONE]
	if !strings.Contains(clientResp, "data: [DONE]") {
		t.Errorf("expected client response to contain [DONE]")
	}
}
