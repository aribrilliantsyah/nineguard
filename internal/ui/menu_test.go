package ui

import (
	"strings"
	"testing"
)

func TestMenuCleanRendering(t *testing.T) {
	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"
	green := "\033[38;2;34;197;94m"
	cyan := "\033[36m"
	muted := "\033[38;5;246m"

	cfg := MenuConfig{
		Version:      "v1.0.0",
		Port:         "8080",
		RouterTarget: "Direct (No upstream router configured)",
	}

	header := []string{
		"  🛡️   " + bold + "NineGuard" + reset + " " + muted + cfg.Version + reset + "   " + green + "● Active" + reset,
		"      " + dim + "Gateway & Firewall for AI Architectures" + reset,
		"",
		"      Server:    " + cyan + "http://localhost:" + cfg.Port + reset,
		"      Upstream:  " + muted + cfg.RouterTarget + reset,
	}

	for i, line := range header {
		clean := StripANSI(line)
		t.Logf("Line %d: %s", i, clean)
		if strings.Contains(clean, "╭") || strings.Contains(clean, "│") || strings.Contains(clean, "╰") {
			t.Errorf("line %d contains box-drawing character: %q", i, clean)
		}
	}
}
