package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWrapBillionContextURL(t *testing.T) {
	bili := "http://127.0.0.1:8788"
	target := "https://api.openai.com/v1/chat/completions"

	wrapped := WrapBillionContextURL(bili, target)
	expected := "http://127.0.0.1:8788/bili/https://api.openai.com/v1/chat/completions"
	if wrapped != expected {
		t.Fatalf("expected %s, got %s", expected, wrapped)
	}

	// No double-wrapping
	doubleWrapped := WrapBillionContextURL(bili, wrapped)
	if doubleWrapped != expected {
		t.Fatalf("expected no double wrap %s, got %s", expected, doubleWrapped)
	}

	// Trailing slash handling
	biliWithSlash := "http://127.0.0.1:8788/"
	wrappedSlash := WrapBillionContextURL(biliWithSlash, target)
	if wrappedSlash != expected {
		t.Fatalf("expected %s, got %s", expected, wrappedSlash)
	}

	// Relative /bili/ path
	wrappedRel := WrapBillionContextURL(bili, "/bili/"+target)
	if wrappedRel != expected {
		t.Fatalf("expected %s, got %s", expected, wrappedRel)
	}
}

func TestApplyBillionContext_CliproxyBackend(t *testing.T) {
	ResetBillionContextHealthCache()
	proxyURL, skipped, skipReason, err := ApplyBillionContext(context.Background(), nil, nil, "cliproxy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skipped || skipReason != "unsupported_backend" {
		t.Fatalf("expected skipped with unsupported_backend, got skipped=%v, reason=%s", skipped, skipReason)
	}
	if proxyURL != "" {
		t.Fatalf("expected empty proxyURL, got %s", proxyURL)
	}
}

func TestApplyBillionContext_InvalidURL(t *testing.T) {
	ResetBillionContextHealthCache()
	settings := map[string]any{"url": "ftp://not-http"}
	_, _, _, err := ApplyBillionContext(context.Background(), nil, settings, "http")
	if err == nil {
		t.Fatal("expected error for invalid url scheme, got nil")
	}
}

func TestApplyBillionContext_HealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantHealth bool
	}{
		{"200 OK", http.StatusOK, true},
		{"204 No Content", http.StatusNoContent, true},
		{"403 Forbidden (Docker loopback-gate)", http.StatusForbidden, true},
		{"404 Not Found", http.StatusNotFound, false},
		{"500 Internal Error", http.StatusInternalServerError, false},
		{"503 Service Unavailable", http.StatusServiceUnavailable, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ResetBillionContextHealthCache()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/__bili/health" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			settings := map[string]any{"url": server.URL}
			proxyURL, skipped, skipReason, err := ApplyBillionContext(context.Background(), server.Client(), settings, "http")

			if tc.wantHealth {
				if err != nil {
					t.Fatalf("expected healthy, got error: %v", err)
				}
				if skipped {
					t.Fatalf("expected not skipped, got reason: %s", skipReason)
				}
				if proxyURL != server.URL {
					t.Fatalf("expected proxyURL %s, got %s", server.URL, proxyURL)
				}
			} else {
				if err == nil {
					t.Fatal("expected error for unhealthy status, got nil")
				}
			}
		})
	}
}

func TestApplyBillionContext_CacheTTL(t *testing.T) {
	ResetBillionContextHealthCache()
	var hitCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	settings := map[string]any{"url": server.URL}
	// First call hits server
	_, _, _, err := ApplyBillionContext(context.Background(), server.Client(), settings, "http")
	if err != nil {
		t.Fatalf("call 1 failed: %v", err)
	}

	// Second call hits in-memory cache
	_, _, _, err = ApplyBillionContext(context.Background(), server.Client(), settings, "http")
	if err != nil {
		t.Fatalf("call 2 failed: %v", err)
	}

	if count := atomic.LoadInt32(&hitCount); count != 1 {
		t.Fatalf("expected 1 health probe request due to TTL cache, got %d", count)
	}

	// Reset cache forces new probe
	ResetBillionContextHealthCache()
	_, _, _, err = ApplyBillionContext(context.Background(), server.Client(), settings, "http")
	if err != nil {
		t.Fatalf("call 3 failed: %v", err)
	}
	if count := atomic.LoadInt32(&hitCount); count != 2 {
		t.Fatalf("expected 2 health probe requests after cache reset, got %d", count)
	}
}
