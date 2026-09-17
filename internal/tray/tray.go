package tray

import (
	"os"
	"runtime"
)

// Options configures the system tray integration.
type Options struct {
	Port            string
	ServerURL       string
	OnOpenDashboard func()
	OnQuit          func()
}

// ModeName returns a human-readable description of current runtime mode.
func ModeName() string {
	if IsSupported() {
		return "Desktop (System Tray)"
	}
	return "Server / Headless (Daemon)"
}

// HasDesktopEnvironment checks whether a graphical desktop environment or window manager session is present.
func HasDesktopEnvironment() bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return true
	}
	// On Linux / Unix, check DISPLAY, WAYLAND_DISPLAY, or desktop session variables
	return os.Getenv("DISPLAY") != "" ||
		os.Getenv("WAYLAND_DISPLAY") != "" ||
		os.Getenv("XDG_CURRENT_DESKTOP") != "" ||
		os.Getenv("DESKTOP_SESSION") != "" ||
		os.Getenv("GDMSESSION") != "" ||
		os.Getenv("WINDOWMANAGER") != ""
}

