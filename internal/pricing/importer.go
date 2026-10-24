package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"nineguard/internal/models"
)

//go:embed prices_snapshot.json
var embeddedPricesSnapshot []byte

type liteLLMEntry struct {
	InputCostPerToken           *float64 `json:"input_cost_per_token"`
	OutputCostPerToken          *float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     *float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost *float64 `json:"cache_creation_input_token_cost"`
	LiteLLMProvider             string   `json:"litellm_provider"`
	Mode                        string   `json:"mode"`
}

var providerPriority = map[string]int{
	"anthropic": 1,
	"openai":    2,
	"gemini":    3,
	"xai":       4,
	"deepseek":  5,
	"mistral":   6,
	"groq":      7,
}

func getProviderRank(provider string) int {
	p := strings.ToLower(strings.TrimSpace(provider))
	if rank, ok := providerPriority[p]; ok {
		return rank
	}
	return 999
}

type candidateModel struct {
	rawKey   string
	provider string
	price    Price
}

func (c candidateModel) isPreferredOver(other candidateModel) bool {
	rankA := getProviderRank(c.provider)
	rankB := getProviderRank(other.provider)
	if rankA != rankB {
		return rankA < rankB
	}
	// Tie breaker 1: shortest raw key length
	if len(c.rawKey) != len(other.rawKey) {
		return len(c.rawKey) < len(other.rawKey)
	}
	// Tie breaker 2: alphabetical
	return c.rawKey < other.rawKey
}

// ParseLiteLLM parses raw LiteLLM JSON into family-keyed Prices per Spec §5.1.
func ParseLiteLLM(data []byte) (map[string]Price, error) {
	var raw map[string]liteLLMEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid LiteLLM JSON: %w", err)
	}

	bestCandidates := make(map[string]candidateModel)

	for key, entry := range raw {
		k := strings.TrimSpace(key)
		if k == "" || strings.EqualFold(k, "sample_spec") {
			continue
		}

		mode := strings.ToLower(strings.TrimSpace(entry.Mode))
		if mode != "chat" && mode != "responses" {
			continue
		}

		if entry.InputCostPerToken == nil || *entry.InputCostPerToken <= 0 ||
			entry.OutputCostPerToken == nil || *entry.OutputCostPerToken <= 0 {
			continue
		}

		fam := models.NormalizeFamily(k)
		if fam == "" {
			continue
		}

		price := Price{
			InputPerMTok:  *entry.InputCostPerToken * 1e6,
			OutputPerMTok: *entry.OutputCostPerToken * 1e6,
		}

		if entry.CacheReadInputTokenCost != nil && *entry.CacheReadInputTokenCost > 0 {
			cr := *entry.CacheReadInputTokenCost * 1e6
			price.CacheReadPerMTok = &cr
		}

		if entry.CacheCreationInputTokenCost != nil && *entry.CacheCreationInputTokenCost > 0 {
			cw := *entry.CacheCreationInputTokenCost * 1e6
			price.CacheWritePerMTok = &cw
		}

		cand := candidateModel{
			rawKey:   k,
			provider: entry.LiteLLMProvider,
			price:    price,
		}

		existing, exists := bestCandidates[fam]
		if !exists || cand.isPreferredOver(existing) {
			bestCandidates[fam] = cand
		}
	}

	results := make(map[string]Price, len(bestCandidates))
	for fam, cand := range bestCandidates {
		results[fam] = cand.price
	}

	return results, nil
}

// LoadSnapshot loads the embedded built-in fallback prices.
func LoadSnapshot() (map[string]Price, error) {
	if len(embeddedPricesSnapshot) == 0 {
		return nil, fmt.Errorf("embedded prices snapshot is empty")
	}
	var prices map[string]Price
	if err := json.Unmarshal(embeddedPricesSnapshot, &prices); err != nil {
		return nil, fmt.Errorf("failed to unmarshal embedded prices snapshot: %w", err)
	}
	return prices, nil
}
