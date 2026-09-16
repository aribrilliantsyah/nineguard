package ui

import (
	"fmt"
)

// Action represents an action selected from the menu.
type Action int

const (
	ActionNone Action = iota
	ActionWeb
	ActionLogs
	ActionTray
	ActionExit
)

type MenuItem struct {
	ID    Action
	Title string
}

// MenuConfig holds dynamic information displayed on the card.
type MenuConfig struct {
	Version      string
	Port         string
	RouterTarget string
}

// ShowMenu displays the interactive selection menu and returns the user's choice.
func ShowMenu(kr *KeyReader, cfg MenuConfig) (Action, error) {
	items := []MenuItem{
		{
			ID:    ActionWeb,
			Title: "Web UI (Open in Browser)",
		},
		{
			ID:    ActionLogs,
			Title: "Running Logs (Live Stream)",
		},
		{
			ID:    ActionTray,
			Title: "Hide to Tray (Background)",
		},
		{
			ID:    ActionExit,
			Title: "Exit",
		},
	}

	kr.Drain()
	selectedIndex := 0

	// Hide cursor during menu navigation
	TermPrint("\033[?25l")

	render := func() {
		// Clear screen and reset cursor to (0,0)
		TermPrint("\033[2J\033[H")

		serverURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
		upstream := cfg.RouterTarget
		if upstream == "" {
			upstream = "Direct (No upstream router configured)"
		}

		reset := "\033[0m"
		bold := "\033[1m"
		dim := "\033[2m"
		green := "\033[38;2;34;197;94m"
		cyan := "\033[36m"
		accent := "\033[38;2;217;119;87m"
		muted := "\033[38;5;246m"

		TermPrintln()
		// Header without box/border
		TermPrintf("  🛡️   %sNineGuard%s %s%s%s   %s● Active%s\n", bold, reset, muted, cfg.Version, reset, green, reset)
		TermPrintf("      %sGateway & Firewall for AI Architectures%s\n\n", dim, reset)
		TermPrintf("      Server:    %s%s%s\n", cyan, serverURL, reset)
		TermPrintf("      Upstream:  %s%s%s\n\n", muted, upstream, reset)

		// Prompt Title
		TermPrintf("  %sChoose interface:%s\n\n", bold, reset)

		// Menu Items (clean, no emojis on buttons)
		for i, it := range items {
			if i == selectedIndex {
				TermPrintf("   %s▸%s  %s%s%s\n", accent, reset, bold, it.Title, reset)
			} else {
				TermPrintf("      %s%s%s\n", dim, it.Title, reset)
			}
		}

		// Footer Hints
		TermPrintln()
		TermPrintf("  %s[↑/↓] Navigate  •  [Enter] Select  •  [1-4] Quick Pick  •  [q] Quit%s\n", dim, reset)
	}

	render()

	for ev := range kr.Events() {
		switch ev.Type {
		case KeyUp:
			selectedIndex--
			if selectedIndex < 0 {
				selectedIndex = len(items) - 1
			}
			render()
		case KeyDown:
			selectedIndex++
			if selectedIndex >= len(items) {
				selectedIndex = 0
			}
			render()
		case KeyEnter:
			return items[selectedIndex].ID, nil
		case KeyCtrlC:
			return ActionExit, nil
		case KeyRune:
			switch ev.Rune {
			case '1':
				return ActionWeb, nil
			case '2':
				return ActionLogs, nil
			case '3':
				return ActionTray, nil
			case '4', 'q', 'Q':
				return ActionExit, nil
			}
		}
	}

	return ActionExit, nil
}
