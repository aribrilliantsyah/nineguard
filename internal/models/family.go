package models

import (
	"regexp"
	"strings"
)

var (
	// Regex for version suffixes: -v1:0, -v1, etc.
	versionSuffixRegex = regexp.MustCompile(`-v\d+(?::\d+)?$`)
	// Regex for date suffixes: @20250929, -20251101, -2024-08-06
	dateSuffixRegex = regexp.MustCompile(`(?:@\d{8}|-\d{8}|-\d{4}-\d{2}-\d{2})$`)
	// Regex for tag suffixes: :thinking, -thinking, :free, :beta, -latest, -high, -medium, -low, -nano, -nonthinking, -n
	tagSuffixRegex = regexp.MustCompile(`(?::thinking|-thinking|:free|:beta|-latest|-high|-medium|-low|-nano|-nonthinking|-n)$`)

	// Repeatable cloud and vendor prefixes
	cloudPrefixes = []string{
		"anthropic.",
		"openai.",
		"google.",
		"us.",
		"eu.",
		"apac.",
		"au.",
		"global.",
	}
)

// NormalizeFamily normalizes a model ID or name into its canonical family representation.
// Used to compare requested vs served models for fallback detection and price lookups.
func NormalizeFamily(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if s == "" {
		return ""
	}

	// 1. Keep segment after last '/'
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}

	// 2. Strip repeatable leading cloud/region/vendor prefixes
	stripped := true
	for stripped {
		stripped = false
		for _, p := range cloudPrefixes {
			if strings.HasPrefix(s, p) {
				s = strings.TrimPrefix(s, p)
				stripped = true
				break
			}
		}
	}

	// 3. Strip tags and suffixes (:thinking, -latest, -v1:0, dates, etc.)
	// Loop in case of combined tags (e.g. -v1:0 or -latest)
	for {
		orig := s
		s = tagSuffixRegex.ReplaceAllString(s, "")
		s = versionSuffixRegex.ReplaceAllString(s, "")
		s = dateSuffixRegex.ReplaceAllString(s, "")
		if s == orig {
			break
		}
	}

	// 4. Replace dots, underscores, and colons with hyphens
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '.' || r == '_' || r == ':' {
			b.WriteRune('-')
		} else {
			b.WriteRune(r)
		}
	}
	s = b.String()

	// 5. Clean up any resulting consecutive or trailing hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")

	return s
}

// IsModelFallback returns true if the served model differs in family from the resolved model ID.
// Returns false if either is empty (no evidence).
func IsModelFallback(resolvedModelID, servedModel string) bool {
	if strings.TrimSpace(resolvedModelID) == "" || strings.TrimSpace(servedModel) == "" {
		return false
	}
	return NormalizeFamily(resolvedModelID) != NormalizeFamily(servedModel)
}
