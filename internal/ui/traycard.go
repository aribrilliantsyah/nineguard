package ui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nineguard/internal/tray"
)

// HandleTraySelection processes the background / tray menu choice.
// Returns shouldExit bool indicating if caller should terminate.
func HandleTraySelection(kr *KeyReader, port string, server *http.Server, onQuit func()) (bool, error) {
	// 1. Server / Headless Mode: Tray is not supported or not needed.
	// NineGuard detaches directly into background daemon mode.
	if !tray.IsSupported() {
		spawned, pid, err := SpawnDaemon(port, false)
		if err == nil && spawned {
			// Stop foreground server so background daemon process can bind port
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
			TermPrintf("  🚀  %sNineGuard Running in Background (Daemon)%s\n\n", bold, reset)
			TermPrintf("   %s●%s Background daemon active %s(PID: %d)%s\n", green, reset, muted, pid, reset)
			TermPrintf("   %s●%s Local Server: %shttp://localhost:%s%s\n", green, reset, cyan, port, reset)
			TermPrintf("   %s●%s Mode: Headless Server Daemon %s(No tray icon needed)%s\n\n", green, reset, dim, reset)
			TermPrintf("   %s💡 You may safely close this terminal window or SSH session.%s\n", dim, reset)
			TermPrintf("   %s   To stream logs: ./nineguard --logs%s\n", dim, reset)
			TermPrintf("   %s   To stop server: kill %d%s\n\n", dim, pid, reset)

			return true, nil
		}

		// Fallback when running via `go run` where detached spawning is not available
		if kr != nil {
			kr.Drain()
		}

		reset := "\033[0m"
		bold := "\033[1m"
		dim := "\033[2m"
		muted := "\033[38;5;246m"
		cyan := "\033[36m"
		green := "\033[38;2;34;197;94m"

		TermPrint("\033[2J\033[H\n")
		TermPrintf("  🚀  %sNineGuard Server Running (Foreground)%s\n\n", bold, reset)
		TermPrintf("   %s●%s Local Server: %shttp://localhost:%s%s\n", green, reset, cyan, port, reset)
		TermPrintf("   %s●%s Mode: Server / Headless (No GUI desktop detected)\n\n", green, reset)
		TermPrintf("   %s💡 To run as a detached background daemon, compile the binary first:%s\n", dim, reset)
		TermPrintf("   %s   make build-server  (or make build)%s\n\n", dim, reset)
		TermPrintf("  %s[Enter / Esc] Back to Menu   •   [q] Stop & Exit%s\n", muted, reset)

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

	// 2. Desktop Mode: Tray is supported! Try spawning background daemon with tray
	spawned, pid, err := SpawnDaemon(port, true)
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

	// In-process tray mode (e.g. go run on desktop)
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

// HandleBackgroundSelection is an alias for HandleTraySelection.
var HandleBackgroundSelection = HandleTraySelection
