package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultFrankfurterURL = "https://api.frankfurter.app/latest"

type FXClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewFXClient(baseURL string) *FXClient {
	if baseURL == "" {
		baseURL = DefaultFrankfurterURL
	}
	return &FXClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type frankfurterResponse struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

func (c *FXClient) FetchRate(ctx context.Context, toCurrency string) (rate float64, date string, err error) {
	cur := strings.ToUpper(strings.TrimSpace(toCurrency))
	if cur == "" || cur == "USD" {
		return 1.0, time.Now().UTC().Format("2006-01-02"), nil
	}

	url := fmt.Sprintf("%s?from=USD&to=%s", c.baseURL, cur)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create fx request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("fx request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("fx request returned HTTP %d", resp.StatusCode)
	}

	var res frankfurterResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, "", fmt.Errorf("failed to decode fx response: %w", err)
	}

	r, ok := res.Rates[cur]
	if !ok || r <= 0 {
		return 0, "", fmt.Errorf("currency %s not found in fx response", cur)
	}

	return r, res.Date, nil
}

// GetEffectiveRate calculates the effective conversion rate, display currency, and source.
// If displayCurrency is USD: rate=1.0, cur="USD", source="usd".
// If manualRate > 0: rate=manualRate, cur=displayCurrency, source="manual".
// Else if autoCurrency == displayCurrency && autoRate > 0: rate=autoRate, cur=displayCurrency, source="auto".
// Else: fallback to rate=1.0, cur="USD", source="none".
func GetEffectiveRate(displayCurrency, manualRateStr, autoRateStr, autoCurrency string) (float64, string, string) {
	cur := strings.ToUpper(strings.TrimSpace(displayCurrency))
	if cur == "" || cur == "USD" {
		return 1.0, "USD", "usd"
	}

	if manualRateStr != "" {
		if mRate, err := strconv.ParseFloat(strings.TrimSpace(manualRateStr), 64); err == nil && mRate > 0 {
			return mRate, cur, "manual"
		}
	}

	if strings.EqualFold(strings.TrimSpace(autoCurrency), cur) && autoRateStr != "" {
		if aRate, err := strconv.ParseFloat(strings.TrimSpace(autoRateStr), 64); err == nil && aRate > 0 {
			return aRate, cur, "auto"
		}
	}

	return 1.0, "USD", "none"
}
