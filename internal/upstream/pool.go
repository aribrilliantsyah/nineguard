package upstream

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FormatSubProviderName standardizes raw sub-provider names for display.
func FormatSubProviderName(raw string) string {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "antigravity":
		return "Antigravity"
	case "copilot", "github_copilot", "github-copilot":
		return "Copilot"
	case "anthropic", "claude":
		return "Anthropic"
	case "google", "gemini":
		return "Google"
	case "openai", "codex":
		return "OpenAI"
	case "deepseek":
		return "DeepSeek"
	case "mistral":
		return "Mistral"
	case "groq":
		return "Groq"
	case "openrouter":
		return "OpenRouter"
	case "kiro":
		return "Kiro"
	default:
		if s == "" {
			return ""
		}
		if strings.ToUpper(s[:1]) != s[:1] {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return s
	}
}

func resolveSubProvider(conn ConnectionQuota) (key, display string) {
	raw := strings.TrimSpace(conn.Provider)
	if raw == "" {
		p := strings.TrimSpace(conn.Plan)
		if p != "" && !strings.EqualFold(p, "Shared Upstream Pool") && !strings.Contains(strings.ToLower(p), "budget") {
			raw = p
		}
	}
	if raw == "" {
		if idx := strings.Index(conn.AccountLabel, " · "); idx != -1 {
			raw = strings.TrimSpace(conn.AccountLabel[idx+len(" · "):])
		}
	}
	if raw == "" {
		return "", ""
	}
	key = strings.ToLower(raw)
	display = FormatSubProviderName(raw)
	return key, display
}

// MergeAccountPools groups connections by their sub-provider identity and aggregates
// each group into a privacy-preserving MergedPoolQuota. Account IDs, counts, and personal emails
// are completely omitted.
func MergeAccountPools(providerName string, connections []ConnectionQuota) []MergedPoolQuota {
	if len(connections) == 0 {
		return nil
	}

	type connGroup struct {
		key         string
		display     string
		connections []ConnectionQuota
	}

	var orderedGroups []*connGroup
	groupMap := make(map[string]*connGroup)

	for _, conn := range connections {
		k, disp := resolveSubProvider(conn)
		grp, exists := groupMap[k]
		if !exists {
			grp = &connGroup{
				key:     k,
				display: disp,
			}
			groupMap[k] = grp
			orderedGroups = append(orderedGroups, grp)
		}
		grp.connections = append(grp.connections, conn)
	}

	hasDistinctSubProviders := len(orderedGroups) > 1 || (len(orderedGroups) == 1 && orderedGroups[0].key != "")

	result := make([]MergedPoolQuota, 0, len(orderedGroups))

	for _, grp := range orderedGroups {
		family := providerName
		subProv := ""
		if hasDistinctSubProviders && grp.display != "" && !strings.EqualFold(grp.display, providerName) {
			family = fmt.Sprintf("%s · %s", providerName, grp.display)
			subProv = grp.key
		}

		merged := MergedPoolQuota{
			Provider:    providerName,
			SubProvider: subProv,
			Family:      family,
			Status:      "ok",
			Buckets:     make([]QuotaBucket, 0),
		}

		type bucketAgg struct {
			used          int64
			total         int64
			remaining     int64
			isCredit      bool
			hasTotal      bool
			earliestReset *time.Time
			order         int
		}

		bucketMap := make(map[string]*bucketAgg)
		orderedKeys := make([]string, 0)
		hasAnyDegraded := false
		hasAnyError := false

		for _, conn := range grp.connections {
			if conn.Status == "degraded" {
				hasAnyDegraded = true
			} else if conn.Status == "error" {
				hasAnyError = true
			}

			for _, b := range conn.Buckets {
				name := strings.TrimSpace(b.Name)
				if name == "" {
					continue
				}

				agg, exists := bucketMap[name]
				if !exists {
					agg = &bucketAgg{
						isCredit: b.IsCreditBalance,
						order:    len(orderedKeys),
					}
					bucketMap[name] = agg
					orderedKeys = append(orderedKeys, name)
				}

				agg.used += b.Used
				agg.total += b.Total
				agg.remaining += b.Remaining
				if b.Total > 0 {
					agg.hasTotal = true
				}

				if b.ResetAt != nil {
					if agg.earliestReset == nil || b.ResetAt.Before(*agg.earliestReset) {
						tCopy := *b.ResetAt
						agg.earliestReset = &tCopy
					}
				}
			}
		}

		if hasAnyError {
			merged.Status = "error"
		} else if hasAnyDegraded {
			merged.Status = "degraded"
		}

		// Build final merged buckets
		for _, name := range orderedKeys {
			agg := bucketMap[name]
			var pct int
			if agg.hasTotal && agg.total > 0 {
				remaining := agg.total - agg.used
				if remaining < 0 {
					remaining = 0
				}
				agg.remaining = remaining
				pct = int(math.Round((float64(remaining) / float64(agg.total)) * 100))
				if pct < 0 {
					pct = 0
				} else if pct > 100 {
					pct = 100
				}
			} else if agg.isCredit {
				pct = 100
			}

			warning := (pct < 25 && agg.hasTotal) || (agg.isCredit && agg.remaining <= 0)
			if warning {
				merged.Warning = true
			}

			merged.Buckets = append(merged.Buckets, QuotaBucket{
				Name:                name,
				Used:                agg.used,
				Total:               agg.total,
				Remaining:           agg.remaining,
				RemainingPercentage: pct,
				ResetAt:             agg.earliestReset,
				IsCreditBalance:     agg.isCredit,
				Warning:             warning,
			})
		}

		result = append(result, merged)
	}

	return result
}

func isBucketAllowed(name string, allowedModels []string) bool {
	nLower := strings.ToLower(name)
	if strings.Contains(nLower, "credit") || strings.Contains(nLower, "balance") {
		return true
	}

	// Specific variant checks: if bucket names a specific variant, user must have access to it
	specificVariants := []string{"opus", "sonnet", "haiku", "gpt-oss", "oss", "image"}
	for _, v := range specificVariants {
		if strings.Contains(nLower, v) {
			hasVariant := false
			for _, am := range allowedModels {
				if strings.Contains(strings.ToLower(am), v) {
					hasVariant = true
					break
				}
			}
			if !hasVariant {
				return false
			}
		}
	}

	// Family matching: gemini, claude, gpt, deepseek, etc.
	families := []string{"gemini", "claude", "gpt", "deepseek", "qwen", "mistral", "llama", "codex", "copilot", "kiro"}
	hasAnyFamilyInBucket := false
	for _, fam := range families {
		if strings.Contains(nLower, fam) {
			hasAnyFamilyInBucket = true
			for _, am := range allowedModels {
				if strings.Contains(strings.ToLower(am), fam) {
					return true
				}
			}
		}
	}

	if hasAnyFamilyInBucket {
		return false
	}

	// Unnamed generic windows (e.g. "5h Session") keep if provider is available
	return true
}

// FilterPoolsByModelAccess filters merged pools according to the client key's allowed models.
func FilterPoolsByModelAccess(pools []MergedPoolQuota, allowedModels []string) []MergedPoolQuota {
	if len(allowedModels) == 0 {
		return pools
	}

	// Check if wildcard "*" is in allowed models
	for _, m := range allowedModels {
		if m == "*" {
			return pools
		}
	}

	filtered := make([]MergedPoolQuota, 0, len(pools))
	for _, pool := range pools {
		// 1. If pool specifies AllowedModels, check provider/model level
		if len(pool.AllowedModels) > 0 {
			matches := false
			for _, pm := range pool.AllowedModels {
				for _, am := range allowedModels {
					if strings.EqualFold(pm, am) || strings.HasPrefix(strings.ToLower(am), strings.ToLower(pm)) {
						matches = true
						break
					}
				}
				if matches {
					break
				}
			}
			if !matches {
				continue
			}
		}

		// 2. Filter individual buckets inside the pool
		if len(pool.Buckets) > 0 {
			var allowedBuckets []QuotaBucket
			for _, b := range pool.Buckets {
				if isBucketAllowed(b.Name, allowedModels) {
					allowedBuckets = append(allowedBuckets, b)
				}
			}

			if len(allowedBuckets) > 0 {
				poolCopy := pool
				poolCopy.Buckets = allowedBuckets
				filtered = append(filtered, poolCopy)
			}
		} else {
			filtered = append(filtered, pool)
		}
	}

	return filtered
}
