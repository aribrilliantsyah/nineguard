package proxy_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxy_HopByHopHeaders_Integration(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	var (
		mu              sync.Mutex
		upstreamHeaders http.Header
		upstreamCalled  bool
	)

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upstreamCalled = true
		upstreamHeaders = r.Header.Clone()
		mu.Unlock()

		// Verify that upstream receives NO hop-by-hop headers
		if val := r.Header.Get("Connection"); val != "" {
			t.Errorf("upstream received Connection header: %q", val)
		}
		if val := r.Header.Get("Keep-Alive"); val != "" {
			t.Errorf("upstream received Keep-Alive header: %q", val)
		}
		if val := r.Header.Get("Upgrade"); val != "" {
			t.Errorf("upstream received Upgrade header: %q", val)
		}
		if val := r.Header.Get("Proxy-Connection"); val != "" {
			t.Errorf("upstream received Proxy-Connection header: %q", val)
		}
		if val := r.Header.Get("X-Custom-Hop"); val != "" {
			t.Errorf("upstream received X-Custom-Hop header (listed in Connection): %q", val)
		}

		// Verify upstream DOES receive end-to-end headers
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("upstream Content-Type = %q, expected 'application/json'", ct)
		}
		if av := r.Header.Get("anthropic-version"); av != "2023-06-01" {
			t.Errorf("upstream anthropic-version = %q, expected '2023-06-01'", av)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer upstream-secret-key" {
			t.Errorf("upstream Authorization = %q, expected 'Bearer upstream-secret-key'", auth)
		}

		// Send upstream response including hop-by-hop headers to verify response filtering
		w.Header().Set("Keep-Alive", "timeout=10")
		w.Header().Set("Upgrade", "h2c")
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"mock\"")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-End-To-End", "safe-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}`))
	}))
	defer mockUpstream.Close()

	_, err = providersMgr.CreateProvider("Mock Anthropic", mockUpstream.URL, "upstream-secret-key", "mock", true, true)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	clientKey, err := keyMgr.CreateKey("test-client", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	reqBody := `{"model":"mock/claude-3-7-sonnet","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Client sends hop-by-hop headers
	req.Header.Set("Connection", "close, X-Custom-Hop")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("X-Custom-Hop", "client-secret-token")

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	called := upstreamCalled
	headers := upstreamHeaders
	mu.Unlock()

	if !called {
		t.Fatal("expected mock upstream to be called")
	}
	if headers.Get("Connection") != "" {
		t.Errorf("upstream received Connection: %q", headers.Get("Connection"))
	}
	if headers.Get("Keep-Alive") != "" {
		t.Errorf("upstream received Keep-Alive: %q", headers.Get("Keep-Alive"))
	}
	if headers.Get("Upgrade") != "" {
		t.Errorf("upstream received Upgrade: %q", headers.Get("Upgrade"))
	}
	if headers.Get("X-Custom-Hop") != "" {
		t.Errorf("upstream received X-Custom-Hop: %q", headers.Get("X-Custom-Hop"))
	}

	// Verify client response headers have upstream hop-by-hop headers removed
	if conn := rec.Header().Get("Connection"); conn != "" {
		t.Errorf("client received Connection response header: %q", conn)
	}
	if ka := rec.Header().Get("Keep-Alive"); ka != "" {
		t.Errorf("client received Keep-Alive response header: %q", ka)
	}
	if up := rec.Header().Get("Upgrade"); up != "" {
		t.Errorf("client received Upgrade response header: %q", up)
	}
	if pa := rec.Header().Get("Proxy-Authenticate"); pa != "" {
		t.Errorf("client received Proxy-Authenticate response header: %q", pa)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("client Content-Type = %q, expected 'application/json'", ct)
	}
	if end := rec.Header().Get("X-End-To-End"); end != "safe-value" {
		t.Errorf("client X-End-To-End = %q, expected 'safe-value'", end)
	}
}

func TestProxy_SequentialStreamingWithConnectionClose(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	var (
		mu           sync.Mutex
		requestCount int
	)

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		currentReq := requestCount
		mu.Unlock()

		// Assert no hop-by-hop headers arrived at upstream
		if val := r.Header.Get("Connection"); val != "" {
			t.Errorf("request %d: upstream received Connection header: %q", currentReq, val)
		}
		if val := r.Header.Get("Keep-Alive"); val != "" {
			t.Errorf("request %d: upstream received Keep-Alive header: %q", currentReq, val)
		}
		if val := r.Header.Get("Upgrade"); val != "" {
			t.Errorf("request %d: upstream received Upgrade header: %q", currentReq, val)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.ResponseWriter to be http.Flusher")
		}

		if strings.HasSuffix(r.URL.Path, "/messages") {
			_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-3-7-sonnet\"}}\n\n"))
			flusher.Flush()

			_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"stream-chunk-%d\"}}\n\n", currentReq)
			flusher.Flush()

			_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n"))
			flusher.Flush()

			_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			flusher.Flush()
		} else {
			// OpenAI /chat/completions format
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"content\":\"stream-chunk-%d\"}}]}\n\n", currentReq)
			flusher.Flush()

			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":5,\"total_tokens\":10}}\n\n"))
			flusher.Flush()

			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
		}
	}))
	defer mockUpstream.Close()

	_, err = providersMgr.CreateProvider("Mock Streaming Provider", mockUpstream.URL, "upstream-secret-key", "mock", true, true)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	clientKey, err := keyMgr.CreateKey("streaming-client", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// 1. Sequential streaming requests to /v1/messages with Connection: close
	t.Run("Anthropic /v1/messages sequential streaming with Connection: close", func(t *testing.T) {
		for i := 1; i <= 6; i++ {
			body := `{"model":"mock/claude-3-7-sonnet","stream":true,"messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connection", "close")
			req.Header.Set("Keep-Alive", "timeout=5")

			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("request %d failed with status %d (expected 200): %s", i, rec.Code, rec.Body.String())
			}

			respBody := rec.Body.String()
			expectedChunk := fmt.Sprintf("stream-chunk-%d", i)
			if !strings.Contains(respBody, expectedChunk) {
				t.Errorf("request %d missing expected chunk %q in body: %s", i, expectedChunk, respBody)
			}
		}
	})

	// 2. Sequential streaming requests to /v1/chat/completions with Connection: close
	t.Run("OpenAI /v1/chat/completions sequential streaming with Connection: close", func(t *testing.T) {
		startCount := requestCount
		for i := 1; i <= 6; i++ {
			body := `{"model":"mock/gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connection", "close")
			req.Header.Set("Keep-Alive", "timeout=5")

			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("request %d failed with status %d (expected 200): %s", i, rec.Code, rec.Body.String())
			}

			respBody := rec.Body.String()
			expectedChunk := fmt.Sprintf("stream-chunk-%d", startCount+i)
			if !strings.Contains(respBody, expectedChunk) {
				t.Errorf("request %d missing expected chunk %q in body: %s", i, expectedChunk, respBody)
			}
		}
	})
}
