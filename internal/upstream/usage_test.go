package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"nineguard/internal/traffic"
	"nineguard/internal/upstream"
)

func TestFetchUpstreamUsage_SuccessAndCache(t *testing.T) {
	var callCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)

		if r.URL.Path != "/v1/usage" {
			http.NotFound(w, r)
			return
		}

		if r.Header.Get("Authorization") != "Bearer secret-key-1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Query().Get("range") != "30d" {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}

		summary := traffic.UsageSummary{
			Range:    "30d",
			Requests: 50,
			Tokens: traffic.TokensBreakdown{
				Total: 100000,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(summary)
	}))
	defer ts.Close()

	mgr := upstream.NewManager()

	// Call 1: fetches from server
	sum1, err := mgr.FetchUpstreamUsage(context.Background(), "prov-1", ts.URL, "secret-key-1", "30d")
	if err != nil {
		t.Fatalf("FetchUpstreamUsage failed: %v", err)
	}
	if sum1 == nil || sum1.Requests != 50 || sum1.Tokens.Total != 100000 {
		t.Fatalf("unexpected summary: %+v", sum1)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", callCount)
	}

	// Call 2: should hit 5-minute cache, NO extra HTTP call
	sum2, err := mgr.FetchUpstreamUsage(context.Background(), "prov-1", ts.URL, "secret-key-1", "30d")
	if err != nil {
		t.Fatalf("FetchUpstreamUsage cached failed: %v", err)
	}
	if sum2 == nil || sum2.Requests != 50 {
		t.Fatalf("unexpected cached summary: %+v", sum2)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestFetchUpstreamUsage_404OlderGateway(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	mgr := upstream.NewManager()

	_, err := mgr.FetchUpstreamUsage(context.Background(), "prov-old", ts.URL, "key", "30d")
	if err == nil {
		t.Fatalf("expected error for 404 response")
	}
}
