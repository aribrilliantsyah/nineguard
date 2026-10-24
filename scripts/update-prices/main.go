package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nineguard/internal/pricing"
)

const defaultPriceURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
const outputPath = "internal/pricing/prices_snapshot.json"

func main() {
	url := defaultPriceURL
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	fmt.Printf("Fetching LiteLLM prices from %s...\n", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch prices: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP request failed with status: %s\n", resp.Status)
		os.Exit(1)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read body: %v\n", err)
		os.Exit(1)
	}

	prices, err := pricing.ParseLiteLLM(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse prices: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(prices, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal snapshot: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	fmt.Printf("Successfully wrote %d model family prices to %s\n", len(prices), outputPath)
}
