package pricing

import (
	"fmt"
	"math"
	"strings"
)

func formatThousands(n int64, sep string) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return sign + s
	}

	var b strings.Builder
	b.WriteString(sign)
	remainder := len(s) % 3
	if remainder > 0 {
		b.WriteString(s[:remainder])
		if len(s) > remainder {
			b.WriteString(sep)
		}
	}
	for i := remainder; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteString(sep)
		}
	}
	return b.String()
}

// FormatCost formats an estimated USD cost with display currency, rate, and unpriced count.
func FormatCost(costUSD float64, currency string, rate float64, unpricedRequests int64) string {
	prefix := ""
	if unpricedRequests > 0 {
		prefix = "≥ "
	}

	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		cur = "USD"
	}
	if rate <= 0 {
		rate = 1.0
	}

	converted := costUSD * rate

	// Sub-cent/sub-unit formatting
	if costUSD > 0 {
		if cur == "IDR" && converted < 1.0 {
			return prefix + "<Rp 1"
		} else if cur != "IDR" && costUSD < 0.01 {
			symbol := "$"
			switch cur {
			case "EUR":
				symbol = "€"
			case "JPY":
				symbol = "¥"
			case "SGD":
				symbol = "S$"
			case "USD":
				symbol = "$"
			default:
				symbol = cur + " "
			}
			return prefix + "<" + symbol + "0.01"
		}
	}

	switch cur {
	case "IDR":
		rounded := int64(math.Round(converted))
		return prefix + "Rp " + formatThousands(rounded, ".")
	case "JPY":
		rounded := int64(math.Round(converted))
		return prefix + "¥" + formatThousands(rounded, ",")
	case "USD":
		rounded := math.Round(converted*100) / 100
		intPart := int64(math.Floor(rounded))
		fracPart := int64(math.Round((rounded - float64(intPart)) * 100))
		if fracPart >= 100 {
			intPart++
			fracPart = 0
		}
		return fmt.Sprintf("%s$%s.%02d", prefix, formatThousands(intPart, ","), fracPart)
	case "EUR":
		rounded := math.Round(converted*100) / 100
		intPart := int64(math.Floor(rounded))
		fracPart := int64(math.Round((rounded - float64(intPart)) * 100))
		if fracPart >= 100 {
			intPart++
			fracPart = 0
		}
		return fmt.Sprintf("%s€%s.%02d", prefix, formatThousands(intPart, ","), fracPart)
	case "SGD":
		rounded := math.Round(converted*100) / 100
		intPart := int64(math.Floor(rounded))
		fracPart := int64(math.Round((rounded - float64(intPart)) * 100))
		if fracPart >= 100 {
			intPart++
			fracPart = 0
		}
		return fmt.Sprintf("%sS$%s.%02d", prefix, formatThousands(intPart, ","), fracPart)
	default:
		rounded := math.Round(converted*100) / 100
		intPart := int64(math.Floor(rounded))
		fracPart := int64(math.Round((rounded - float64(intPart)) * 100))
		if fracPart >= 100 {
			intPart++
			fracPart = 0
		}
		return fmt.Sprintf("%s%s %s.%02d", prefix, cur, formatThousands(intPart, ","), fracPart)
	}
}
