package proxy_test

import (
	"net/http"
	"testing"

	"nineguard/internal/proxy"
)

func TestRemoveHopByHopHeaders_StaticHeaders(t *testing.T) {
	staticHopHeaders := []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}

	h := make(http.Header)
	for _, hdr := range staticHopHeaders {
		h.Set(hdr, "some-value")
	}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer token123")

	proxy.RemoveHopByHopHeaders(h)

	for _, hdr := range staticHopHeaders {
		if val := h.Get(hdr); val != "" {
			t.Errorf("expected hop-by-hop header %q to be removed, but got %q", hdr, val)
		}
	}

	if got := h.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type was modified: got %q, expected %q", got, "application/json")
	}
	if got := h.Get("Authorization"); got != "Bearer token123" {
		t.Errorf("Authorization was modified: got %q, expected %q", got, "Bearer token123")
	}
}

func TestRemoveHopByHopHeaders_ConnectionTokens(t *testing.T) {
	tests := []struct {
		name            string
		connectionVal   string
		extraHeaders    map[string]string
		expectedDropped []string
		expectedKept    map[string]string
	}{
		{
			name:          "single custom token in Connection",
			connectionVal: "close, X-Foo",
			extraHeaders: map[string]string{
				"X-Foo":        "bar",
				"Content-Type": "application/json",
			},
			expectedDropped: []string{"Connection", "X-Foo"},
			expectedKept: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name:          "multiple tokens in Connection with spaces and quotes",
			connectionVal: `close, "X-Custom-1",  X-Custom-2 , Keep-Alive`,
			extraHeaders: map[string]string{
				"X-Custom-1": "val1",
				"X-Custom-2": "val2",
				"X-Keep":     "val3",
			},
			expectedDropped: []string{"Connection", "X-Custom-1", "X-Custom-2"},
			expectedKept: map[string]string{
				"X-Keep": "val3",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			h.Set("Connection", tc.connectionVal)
			for k, v := range tc.extraHeaders {
				h.Set(k, v)
			}

			proxy.RemoveHopByHopHeaders(h)

			for _, dropped := range tc.expectedDropped {
				if val := h.Get(dropped); val != "" {
					t.Errorf("expected header %q to be dropped, got %q", dropped, val)
				}
			}
			for k, exp := range tc.expectedKept {
				if got := h.Get(k); got != exp {
					t.Errorf("expected header %q to be %q, got %q", k, exp, got)
				}
			}
		})
	}
}

func TestRemoveHopByHopHeaders_MultipleConnectionHeaders(t *testing.T) {
	h := make(http.Header)
	h.Add("Connection", "close, X-Token-1")
	h.Add("Connection", "X-Token-2, Upgrade")
	h.Set("X-Token-1", "value1")
	h.Set("X-Token-2", "value2")
	h.Set("X-Normal", "ok")

	proxy.RemoveHopByHopHeaders(h)

	if val := h.Get("Connection"); val != "" {
		t.Errorf("expected Connection to be removed, got %q", val)
	}
	if val := h.Get("X-Token-1"); val != "" {
		t.Errorf("expected X-Token-1 to be removed, got %q", val)
	}
	if val := h.Get("X-Token-2"); val != "" {
		t.Errorf("expected X-Token-2 to be removed, got %q", val)
	}
	if got := h.Get("X-Normal"); got != "ok" {
		t.Errorf("expected X-Normal to remain 'ok', got %q", got)
	}
}

func TestRemoveHopByHopHeaders_CaseInsensitivity(t *testing.T) {
	// Directly construct map with non-canonical casing to test robustness
	h := http.Header{
		"connection": []string{"CLOSE, x-custom-token"},
		"UPGRADE":    []string{"websocket"},
		"keep-alive": []string{"timeout=5"},
		"X-CUSTOM-TOKEN": []string{"custom-value"},
		"proxy-connection": []string{"keep-alive"},
		"content-type": []string{"application/json"},
	}

	proxy.RemoveHopByHopHeaders(h)

	if len(h["connection"]) > 0 || h.Get("Connection") != "" {
		t.Errorf("expected connection to be removed, got: %v", h["connection"])
	}
	if len(h["UPGRADE"]) > 0 || h.Get("Upgrade") != "" {
		t.Errorf("expected UPGRADE to be removed, got: %v", h["UPGRADE"])
	}
	if len(h["keep-alive"]) > 0 || h.Get("Keep-Alive") != "" {
		t.Errorf("expected keep-alive to be removed, got: %v", h["keep-alive"])
	}
	if len(h["X-CUSTOM-TOKEN"]) > 0 || h.Get("x-custom-token") != "" {
		t.Errorf("expected X-CUSTOM-TOKEN to be removed, got: %v", h["X-CUSTOM-TOKEN"])
	}
	if len(h["proxy-connection"]) > 0 || h.Get("Proxy-Connection") != "" {
		t.Errorf("expected proxy-connection to be removed, got: %v", h["proxy-connection"])
	}

	// Content-Type should remain
	if val := h.Get("Content-Type"); val != "application/json" {
		if val2 := h["content-type"]; len(val2) == 0 || val2[0] != "application/json" {
			t.Errorf("expected content-type to be preserved, got: %v", h)
		}
	}
}

func TestRemoveHopByHopHeaders_TETrailersDropped(t *testing.T) {
	h := make(http.Header)
	h.Set("TE", "trailers")
	h.Set("Content-Type", "application/json")

	proxy.RemoveHopByHopHeaders(h)

	if val := h.Get("TE"); val != "" {
		t.Errorf("expected TE: trailers to be simply dropped, but got %q", val)
	}
	if val := h.Get("Te"); val != "" {
		t.Errorf("expected Te to be dropped, but got %q", val)
	}
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected Content-Type to remain, got %q", got)
	}
}

func TestRemoveHopByHopHeaders_NilAndEmpty(t *testing.T) {
	// Should not panic on nil
	proxy.RemoveHopByHopHeaders(nil)

	// Should handle empty header gracefully
	empty := make(http.Header)
	proxy.RemoveHopByHopHeaders(empty)
	if len(empty) != 0 {
		t.Errorf("expected empty header map to stay empty, got: %v", empty)
	}

	// Should handle empty Connection value gracefully
	h := make(http.Header)
	h.Set("Connection", "  ,  ,  ")
	proxy.RemoveHopByHopHeaders(h)
	if len(h) != 0 {
		t.Errorf("expected header map to be empty, got: %v", h)
	}
}

func TestRemoveHopByHopHeaders_NonHopHeadersUntouched(t *testing.T) {
	h := make(http.Header)
	expected := map[string]string{
		"Content-Type":                 "application/json",
		"Content-Length":               "123",
		"Authorization":                "Bearer secret-token",
		"anthropic-version":            "2023-06-01",
		"anthropic-beta":               "prompt-caching-2024-07-31",
		"User-Agent":                   "CustomClient/1.0",
		"Accept":                       "application/json",
		"X-NineGuard-Plugins-Applied":  "test-plugin",
		"X-NineGuard-Trace":            "inst-1",
		"X-NineGuard-Hop":              "1",
	}

	for k, v := range expected {
		h.Set(k, v)
	}

	proxy.RemoveHopByHopHeaders(h)

	for k, exp := range expected {
		if got := h.Get(k); got != exp {
			t.Errorf("header %q modified: got %q, expected %q", k, got, exp)
		}
	}
}
