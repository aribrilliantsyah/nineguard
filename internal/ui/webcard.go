package ui

import (
	"fmt"
)

// ShowWebPrompt opens the browser and displays a clean return/switch prompt.
func ShowWebPrompt(kr *KeyReader, port string) (Action, error) {
	url := fmt.Sprintf("http://localhost:%s", port)
	_ = OpenBrowser(url)

	if kr == nil {
		return ActionNone, nil
	}

	kr.Drain()

	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"
	cyan := "\033[36m"
	muted := "\033[38;5;246m"

	TermPrint("\033[2J\033[H\n")
	TermPrintf("  🌐  %sDashboard Opened in Browser%s\n", bold, reset)
	TermPrintf("      %s%s%s\n\n", cyan, url, reset)
	TermPrintf("      %sNineGuard proxy is running actively in the background.%s\n\n", dim, reset)
	TermPrintf("  %s[Enter] Back to Menu   •   [l] View Running Logs   •   [q] Exit%s\n", muted, reset)

	for ev := range kr.Events() {
		switch ev.Type {
		case KeyEnter, KeyEsc, KeyBackspace:
			return ActionNone, nil
		case KeyCtrlC:
			return ActionExit, nil
		case KeyRune:
			switch ev.Rune {
			case 'l', 'L', '2':
				return ActionLogs, nil
			case 'q', 'Q':
				return ActionExit, nil
			}
		}
	}

	return ActionNone, nil
}
