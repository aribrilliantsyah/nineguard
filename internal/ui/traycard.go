package ui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nineguard/internal/tray"
)

// HandleTraySelection processes the "Hide to Tray" menu choice.
// Returns shouldExit bool indicating if caller should terminate.
func HandleTraySelection(kr *KeyReader, port string, server *http.Server, onQuit func()) (bool, error) {
	if !tray.IsSupported() {
		if kr != nil {
			kr.Drain()
		}

		reset := "\033[0m"
		bold := "\033[1m"
		dim := "\033[2m"
		muted := "\033[38;5;246m"
		yellow := "\033[33m"

		TermPrint("\033[2J\033[H\n")
		TermPrintf("  🔔  %s%sSystem Tray Unavailable%s\n", yellow, bold, reset)
		TermPrintf("      %sNo graphical desktop session (X11/Wayland) found.%s\n\n", dim, reset)
		TermPrintf("      %sNineGuard continues running in this terminal.%s\n\n", dim, reset)
		TermPrintf("  %s[Enter / Esc] Back to Menu   •   [q] Exit%s\n", muted, reset)

		if kr != nil {
			for ev := range kr.Events() {
				switch ev.Type {
				case KeyEnter, KeyEsc, KeyBackspace:
					return false, nil
				case KeyCtrlC:
					return true, nil
				case KeyRune:
					if ev.Rune == 'q' || ev.Rune == 'Q' {
						return true, nil
					}
					if ev.Rune == 'b' || ev.Rune == 'B' {
						return false, nil
					}
				}
			}
		}
		return false, nil
	}

	// Tray is supported! Try spawning background daemon
	spawned, pid, err := SpawnDaemon(port)
	if err == nil && spawned {
		// Stop foreground server so background process can bind port
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(ctx)
		cancel()

		reset := "\033[0m"
		bold := "\033[1m"
		dim := "\033[2m"
		green := "\033[38;2;34;197;94m"
		cyan := "\033[36m"
		muted := "\033[38;5;246m"

		TermPrint("\033[2J\033[H\n")
		TermPrintf("  🔔  %sNineGuard Sent to System Tray%s\n\n", bold, reset)
		TermPrintf("   %s●%s Background daemon active %s(PID: %d)%s\n", green, reset, muted, pid, reset)
		TermPrintf("   %s●%s Local Server: %shttp://localhost:%s%s\n", green, reset, cyan, port, reset)
		TermPrintf("   %s●%s Look for the NineGuard shield icon in your system tray.\n\n", green, reset)
		TermPrintf("   %s💡 You may safely close this terminal window.%s\n", dim, reset)
		TermPrintf("   %s   Right-click the tray icon to Open Dashboard or Quit.%s\n\n", dim, reset)

		return true, nil
	}

	// In-process tray mode (e.g. go run)
	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"
	green := "\033[38;2;34;197;94m"
	cyan := "\033[36m"

	TermPrint("\033[2J\033[H\n")
	TermPrintf("  🔔  %sNineGuard Running in System Tray%s\n\n", bold, reset)
	TermPrintf("   %s●%s Tray icon active in your notification area\n", green, reset)
	TermPrintf("   %s●%s Local Server: %shttp://localhost:%s%s\n", green, reset, cyan, port, reset)
	TermPrintf("   %s●%s Right-click the tray icon to Open Dashboard or Quit.\n\n", green, reset)
	TermPrintf("   %sPress Ctrl+C in this terminal anytime to stop.%s\n\n", dim, reset)

	// Run tray directly
	tray.Run(tray.Options{
		Port:      port,
		ServerURL: fmt.Sprintf("http://localhost:%s", port),
		OnOpenDashboard: func() {
			_ = OpenBrowser(fmt.Sprintf("http://localhost:%s", port))
		},
		OnQuit: func() {
			if onQuit != nil {
				onQuit()
			}
		},
	})

	return true, nil
}
