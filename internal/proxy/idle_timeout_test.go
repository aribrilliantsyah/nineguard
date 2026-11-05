package proxy_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestIdleStreamTimeout(t *testing.T) {
	// Set idle timeout short for testing
	origIdle := proxy.StreamIdleTimeout
	proxy.StreamIdleTimeout = 100 * time.Millisecond
	defer func() {
		proxy.StreamIdleTimeout = origIdle
	}()

	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	// Case 1: Active stream sending chunks every 30ms (shorter than 100ms idle timeout) for 250ms total.
	// Should complete normally without idle timeout.
	upstreamActive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"chunk\"}}]}\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstreamActive.Close()

	provActive, _ := providersMgr.CreateProvider("prov-active", upstreamActive.URL, "sk-test", "", true, true)
	clientKey, _ := keyMgr.CreateKey("client-agent", "all", nil, nil)
	p, _ := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req1.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("active stream failed: code %d: %s", rec1.Code, rec1.Body.String())
	}
	time.Sleep(50 * time.Millisecond)

	logs, _, _ := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if len(logs) == 0 {
		t.Fatalf("expected log entry for active stream")
	}
	if logs[0].ErrorMessage != nil && strings.Contains(*logs[0].ErrorMessage, "upstream idle timeout") {
		t.Errorf("active stream should not have idle timeout, got error: %s", *logs[0].ErrorMessage)
	}

	// Case 2: Upstream sends one chunk then stalls for 250ms (> 100ms idle timeout)
	upstreamStall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// stall until client cancels
		<-r.Context().Done()
	}))
	defer upstreamStall.Close()

	_, _ = providersMgr.UpdateProvider(provActive.ID, "prov-active", upstreamStall.URL, "sk-test", "", true, true)

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req2.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)

	time.Sleep(50 * time.Millisecond)
	logs2, _, _ := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 1})
	if len(logs2) == 0 {
		t.Fatalf("expected log entry for stalled stream")
	}
	if logs2[0].ErrorMessage == nil || !strings.Contains(*logs2[0].ErrorMessage, "upstream idle timeout") {
		errMsg := ""
		if logs2[0].ErrorMessage != nil {
			errMsg = *logs2[0].ErrorMessage
		}
		t.Fatalf("expected 'upstream idle timeout' in error message, got: %q", errMsg)
	}

	// Case 3: Final SSE event without trailing blank line reaches the client
	upstreamTrailing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// Final event has NO blank line after it
		_, _ = w.Write([]byte("data: [DONE]\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstreamTrailing.Close()

	provTrailing, _ := providersMgr.CreateProvider("prov-trailing", upstreamTrailing.URL, "sk-test", "", true, true)
	pTrailing, _ := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	_ = provTrailing

	req3 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(reqBody))
	req3.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	pTrailing.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("trailing stream failed: code %d: %s", rec3.Code, rec3.Body.String())
	}
	if !strings.Contains(rec3.Body.String(), "data: [DONE]") {
		t.Fatalf("expected client response to contain final SSE event 'data: [DONE]', got: %q", rec3.Body.String())
	}
}
