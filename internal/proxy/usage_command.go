package proxy

import (
	"fmt"
	"strings"

	"nineguard/internal/pricing"
	"nineguard/internal/traffic"
)

func formatTokensBrief(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// FormatUsageMarkdown generates user-facing Markdown for /usage per Spec §6.6.
func FormatUsageMarkdown(s *traffic.UsageSummary, arg string, showCost bool) string {
	if s == nil {
		return "No usage data recorded for this API key."
	}

	var sb strings.Builder

	argClean := strings.ToLower(strings.TrimSpace(arg))
	if argClean != "" && argClean != "7d" && argClean != "30d" && argClean != "all" {
		sb.WriteString(fmt.Sprintf("Unknown range %q, showing 30d. Use /usage 7d|30d|all.\n\n", arg))
	}

	rangeTitle := "last 30 days"
	if s.Range == "7d" {
		rangeTitle = "last 7 days"
	} else if s.Range == "all" {
		rangeTitle = "all time"
	}

	tzStr := s.Timezone
	if tzStr == "" {
		tzStr = "UTC"
	}

	sb.WriteString(fmt.Sprintf("📈 **NineGuard Usage — %s (%s)**\n\n", rangeTitle, tzStr))

	// KPI line 1
	errPctStr := "0%"
	if s.Requests+s.Errors > 0 {
		errPctStr = fmt.Sprintf("%.1f%%", s.ErrorRatePct)
	}
	sb.WriteString(fmt.Sprintf("Requests %s · Errors %s (%s) · Sessions %s (inferred) · Active days %s\n",
		formatTokenCount(s.Requests),
		formatTokenCount(s.Errors),
		errPctStr,
		formatTokenCount(s.Sessions),
		formatTokenCount(s.ActiveDays),
	))

	// KPI line 2: Tokens & Cost
	tokenLine := fmt.Sprintf("Tokens %s (in %s · out %s · cache hit %.0f%%)",
		formatTokensBrief(s.Tokens.Total),
		formatTokensBrief(s.Tokens.Prompt),
		formatTokensBrief(s.Tokens.Completion),
		s.CacheHitPct,
	)
	if showCost && s.Cost != nil {
		costStr := s.Cost.DisplayFormatted
		if costStr == "" {
			costStr = fmt.Sprintf("$%.2f", s.Cost.USD)
		}
		tokenLine += " · Est. cost " + costStr
	}
	sb.WriteString(tokenLine + "\n")

	// KPI line 3: Peak hour & favorite model
	favMod := s.FavoriteModel
	if favMod == "" {
		favMod = "-"
	}
	sb.WriteString(fmt.Sprintf("Peak hour %s · Favorite model %s\n", s.PeakHourFormatted, favMod))

	// KPI line 4: Thinking & fallbacks
	sb.WriteString(fmt.Sprintf("Thinking: requested %s · delivered %s · lost %s · Fallbacks %s\n\n",
		formatTokenCount(s.Thinking.Requested),
		formatTokenCount(s.Thinking.Delivered),
		formatTokenCount(s.Thinking.Lost),
		formatTokenCount(s.Fallbacks),
	))

	// Models Table
	if len(s.Models) > 0 {
		if showCost && s.Cost != nil {
			curLabel := s.Cost.Currency
			if curLabel == "" {
				curLabel = "USD"
			}
			sb.WriteString(fmt.Sprintf("| Model | In | Out | Est. cost (%s) | Share |\n", curLabel))
			sb.WriteString("|---|---|---|---|---|\n")
			for _, m := range s.Models {
				cStr := "—"
				if m.Cost != nil {
					cStr = pricing.FormatCost(*m.Cost, s.Cost.Currency, s.Cost.Rate, 0)
				}
				sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %.1f%% |\n",
					m.ModelID,
					formatTokensBrief(m.Prompt),
					formatTokensBrief(m.Completion),
					cStr,
					m.PctTokens,
				))
			}
		} else {
			sb.WriteString("| Model | In | Out | Share |\n")
			sb.WriteString("|---|---|---|---|\n")
			for _, m := range s.Models {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s | %.1f%% |\n",
					m.ModelID,
					formatTokensBrief(m.Prompt),
					formatTokensBrief(m.Completion),
					m.PctTokens,
				))
			}
		}
		sb.WriteString("\n")
	}

	// Upstream Usage
	if showCost && len(s.UpstreamUsage) > 0 {
		for _, u := range s.UpstreamUsage {
			if u.Summary != nil {
				cStr := ""
				if u.Summary.Cost != nil && u.Summary.Cost.DisplayFormatted != "" {
					cStr = " · Est. cost " + u.Summary.Cost.DisplayFormatted
				}
				sb.WriteString(fmt.Sprintf("Upstream (%s, shared by all users of this gateway): %s tokens%s\n",
					u.Provider,
					formatTokensBrief(u.Summary.Tokens.Total),
					cStr,
				))
			}
		}
	}

	return strings.TrimSpace(sb.String())
}
