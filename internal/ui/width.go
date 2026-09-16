package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\([B]`)

// StripANSI removes ANSI escape codes from string.
func StripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// VisualWidth estimates the visual column width in standard monospace terminals.
func VisualWidth(s string) int {
	clean := StripANSI(s)
	width := 0
	for _, r := range clean {
		switch {
		case r == 0xFE0F: // variation selector-16 (emoji)
			continue
		case r >= 0x1F300 && r <= 0x1FAFF: // Miscellaneous Symbols, Pictographs, Emoticons
			width += 2
		case r >= 0x2600 && r <= 0x26FF: // Miscellaneous Symbols
			width += 2
		case r >= 0x2700 && r <= 0x27BF: // Dingbats
			if r == '✕' {
				width += 1
			} else {
				width += 2
			}
		default:
			width += 1
		}
	}
	return width
}

// PadRow ensures the rendered string occupies EXACTLY targetWidth terminal cells.
func PadRow(s string, targetWidth int) string {
	vw := VisualWidth(s)
	if vw < targetWidth {
		return s + strings.Repeat(" ", targetWidth-vw)
	}
	if vw > targetWidth {
		clean := StripANSI(s)
		if len(clean) > targetWidth-3 {
			return clean[:targetWidth-3] + "..."
		}
		return clean[:targetWidth]
	}
	return s
}

// TermPrint ensures that all newlines in the string are translated to \r\n (Carriage Return + Line Feed).
// In terminal raw mode, a bare \n causes the cursor to only move down without returning to column 0,
// resulting in the infamous staircase effect.
func TermPrint(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	_, _ = os.Stdout.WriteString(s)
}

// TermPrintf formats text and writes it via TermPrint.
func TermPrintf(format string, a ...any) {
	TermPrint(fmt.Sprintf(format, a...))
}

// TermPrintln writes text followed by a carriage-returned newline.
func TermPrintln(a ...any) {
	TermPrint(fmt.Sprintln(a...))
}

// RuneCount returns the number of runes in a clean string.
func RuneCount(s string) int {
	return utf8.RuneCountInString(StripANSI(s))
}
