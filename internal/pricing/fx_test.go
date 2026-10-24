package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchFrankfurterRate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("to") == "IDR" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"amount":1.0,"base":"USD","date":"2026-10-16","rates":{"IDR":18031.5}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// Inject test server URL
	client := NewFXClient(ts.URL)

	rate, date, err := client.FetchRate(context.Background(), "IDR")
	if err != nil {
		t.Fatalf("unexpected error fetching rate: %v", err)
	}
	if !floatEquals(rate, 18031.5) {
		t.Fatalf("expected rate 18031.5, got %v", rate)
	}
	if date != "2026-10-16" {
		t.Fatalf("expected date 2026-10-16, got %s", date)
	}

	// USD returns 1.0 immediately
	usdRate, _, err := client.FetchRate(context.Background(), "USD")
	if err != nil {
		t.Fatalf("unexpected error for USD: %v", err)
	}
	if usdRate != 1.0 {
		t.Fatalf("expected 1.0 for USD, got %v", usdRate)
	}
}

func TestGetEffectiveRate(t *testing.T) {
	// 1. USD display currency
	rate, cur, src := GetEffectiveRate("USD", "15000", "16000", "IDR")
	if rate != 1.0 || cur != "USD" || src != "usd" {
		t.Fatalf("expected USD effective rate 1.0, got rate=%v, cur=%s, src=%s", rate, cur, src)
	}

	// 2. Manual rate takes priority over auto rate
	rate, cur, src = GetEffectiveRate("IDR", "17500", "18031.5", "IDR")
	if rate != 17500 || cur != "IDR" || src != "manual" {
		t.Fatalf("expected manual rate priority, got rate=%v, cur=%s, src=%s", rate, cur, src)
	}

	// 3. Auto rate when manual is empty
	rate, cur, src = GetEffectiveRate("IDR", "", "18031.5", "IDR")
	if rate != 18031.5 || cur != "IDR" || src != "auto" {
		t.Fatalf("expected auto rate, got rate=%v, cur=%s, src=%s", rate, cur, src)
	}

	// 4. Fallback to USD when currency has no valid rate
	rate, cur, src = GetEffectiveRate("EUR", "", "18031.5", "IDR")
	if rate != 1.0 || cur != "USD" || src != "none" {
		t.Fatalf("expected USD fallback when auto rate currency mismatch, got rate=%v, cur=%s, src=%s", rate, cur, src)
	}
}
