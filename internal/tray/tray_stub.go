//go:build (!cgo && !windows) || (!linux && !darwin && !windows) || server || headless || notray

package tray

import (
	"log/slog"
)

// IsSupported returns false in headless, server, or non-cgo builds.
func IsSupported() bool {
	return false
}

// Run is a no-op fallback when system tray is unavailable.
func Run(opts Options) {
	slog.Warn("system tray is not supported in this build or environment")
}

// Quit is a no-op fallback.
func Quit() {}

