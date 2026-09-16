package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// ViewLogs runs an interactive live log viewer.
// It returns true if the user requested an application exit (e.g. pressed 'q' or Ctrl+C).
func ViewLogs(kr *KeyReader, hub *LogHub) (bool, error) {
	subID, logCh := hub.Subscribe()
	defer hub.Unsubscribe(subID)

	isTTY := isatty.IsTerminal(os.Stdin.Fd())
	if !isTTY || kr == nil {
		// Non-interactive streaming fallback
		for _, entry := range hub.Recent() {
			TermPrintf("%s [%s] [%s] %s\n", entry.Timestamp.Format("15:04:05"), entry.Level, entry.Source, entry.Message)
		}
		for entry := range logCh {
			TermPrintf("%s [%s] [%s] %s\n", entry.Timestamp.Format("15:04:05"), entry.Level, entry.Source, entry.Message)
		}
		return false, nil
	}

	kr.Drain()

	border := "\033[38;5;241m"
	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"
	muted := "\033[38;5;246m"

	renderHeader := func() {
		TermPrint("\033[2J\033[H\n")
		TermPrintf("  📋  %sNineGuard Live Logs%s\n", bold, reset)
		TermPrintf("      %sStreaming server events & proxy traffic in real-time%s\n\n", dim, reset)
		TermPrintf("  %s[Esc / Enter] Back to Menu   •   [c] Clear   •   [q] Exit%s\n", muted, reset)
		TermPrintf("  %s%s%s\n\n", border, strings.Repeat("─", 62), reset)
	}

	renderEntry := func(entry LogEntry) {
		timeStr := entry.Timestamp.Format("15:04:05")

		var lvlTag string
		switch strings.ToUpper(entry.Level) {
		case "INFO":
			lvlTag = "\033[32m● INFO \033[0m"
		case "WARN":
			lvlTag = "\033[33m▲ WARN \033[0m"
		case "ERROR", "FATAL":
			lvlTag = "\033[31m✖ ERROR\033[0m"
		case "DEBUG":
			lvlTag = "\033[35m◆ DEBUG\033[0m"
		default:
			lvlTag = fmt.Sprintf("  %-5s", entry.Level)
		}

		src := entry.Source
		if src == "" {
			src = "server"
		}
		srcTag := fmt.Sprintf("\033[38;5;244m[%-6s]\033[0m", src)

		var attrParts []string
		for k, v := range entry.Attrs {
			attrParts = append(attrParts, fmt.Sprintf("%s\033[38;5;242m=%v\033[0m", k, v))
		}
		var attrStr string
		if len(attrParts) > 0 {
			attrStr = " " + strings.Join(attrParts, " ")
		}

		TermPrintf("  \033[2m%s\033[0m %s %s %s%s\n", timeStr, lvlTag, srcTag, entry.Message, attrStr)
	}

	renderHeader()

	// Show recent history
	for _, entry := range hub.Recent() {
		renderEntry(entry)
	}

	for {
		select {
		case entry, ok := <-logCh:
			if !ok {
				return false, nil
			}
			renderEntry(entry)
		case ev, ok := <-kr.Events():
			if !ok {
				return false, nil
			}
			switch ev.Type {
			case KeyEsc, KeyEnter, KeyBackspace:
				return false, nil
			case KeyCtrlC:
				return true, nil
			case KeyRune:
				switch ev.Rune {
				case 'q', 'Q':
					return true, nil
				case 'c', 'C':
					renderHeader()
				case 'b', 'B':
					return false, nil
				}
			}
		case <-time.After(50 * time.Millisecond):
			// Keep loop responsive
		}
	}
}
