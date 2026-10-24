package traffic

import (
	"fmt"
	"math"
	"strings"
)

type bookEntry struct {
	title  string
	words  int64
	tokens int64
}

// Built-in literature table per Spec §6.5 (sorted ascending by tokens)
var knownBooks = []bookEntry{
	{title: "The Little Prince", words: 17000, tokens: 17000 * 4 / 3},
	{title: "Animal Farm", words: 30000, tokens: 30000 * 4 / 3},
	{title: "Harry Potter and the Philosopher's Stone", words: 77000, tokens: 77000 * 4 / 3},
	{title: "The Hobbit", words: 95000, tokens: 95000 * 4 / 3},
	{title: "Pride and Prejudice", words: 122000, tokens: 122000 * 4 / 3},
	{title: "Moby-Dick", words: 206000, tokens: 206000 * 4 / 3},
	{title: "The Lord of the Rings", words: 481000, tokens: 481000 * 4 / 3},
	{title: "War and Peace", words: 587000, tokens: 587000 * 4 / 3},
	{title: "The Bible (KJV)", words: 783000, tokens: 783000 * 4 / 3},
	{title: "English Wikipedia", words: 4500000000, tokens: 4500000000 * 4 / 3},
}

func formatWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteString(",")
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteString(",")
		}
	}
	return b.String()
}

// CompareTokens compares a total token count against literature per Spec §6.5.
func CompareTokens(totalTokens int64) *ComparisonResult {
	if totalTokens <= 0 || len(knownBooks) == 0 {
		return nil
	}

	smallest := knownBooks[0]
	if totalTokens < smallest.tokens {
		pct := int64(math.Round(float64(totalTokens) / float64(smallest.tokens) * 100))
		if pct < 1 {
			pct = 1
		}
		mult := float64(totalTokens) / float64(smallest.tokens)
		return &ComparisonResult{
			Book:     smallest.title,
			Multiple: mult,
			Text:     fmt.Sprintf("You've used ~%d%% of the tokens in %s.", pct, smallest.title),
		}
	}

	var chosen bookEntry
	for i := len(knownBooks) - 1; i >= 0; i-- {
		if totalTokens >= knownBooks[i].tokens {
			chosen = knownBooks[i]
			break
		}
	}

	mult := float64(totalTokens) / float64(chosen.tokens)
	var multStr string
	if mult < 10.0 {
		multStr = fmt.Sprintf("%.1f", mult)
	} else {
		rounded := int64(math.Round(mult))
		multStr = formatWithCommas(rounded)
	}

	return &ComparisonResult{
		Book:     chosen.title,
		Multiple: mult,
		Text:     fmt.Sprintf("You've used ~%s× more tokens than %s.", multStr, chosen.title),
	}
}
