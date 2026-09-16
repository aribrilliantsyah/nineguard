//go:build (linux || darwin || windows) && (cgo || windows)

package tray

import (
	"log/slog"
	"os"
	"runtime"

	"github.com/getlantern/systray"
	"nineguard/web"
)

// IsSupported returns true if the current environment can display a system tray.
func IsSupported() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return true
	case "linux":
		// On Linux, a GUI session (X11 or Wayland) is required
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default:
		return false
	}
}

// Run starts the system tray loop. This call blocks until Quit is called.
func Run(opts Options) {
	onReady := func() {
		iconBytes := web.GetFaviconBytes()
		if len(iconBytes) > 0 {
			systray.SetIcon(iconBytes)
		}
		systray.SetTitle("NineGuard")
		systray.SetTooltip("NineGuard AI Gateway (Port " + opts.Port + ")")

		mHeader := systray.AddMenuItem("NineGuard (Port "+opts.Port+")", "NineGuard AI Gateway")
		mHeader.Disable()

		systray.AddSeparator()

		mOpen := systray.AddMenuItem("Open Dashboard", "Open NineGuard Web UI in browser")
		mQuit := systray.AddMenuItem("Quit NineGuard", "Stop NineGuard and exit")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if opts.OnOpenDashboard != nil {
						opts.OnOpenDashboard()
					}
				case <-mQuit.ClickedCh:
					if opts.OnQuit != nil {
						opts.OnQuit()
					}
					systray.Quit()
					return
				}
			}
		}()
	}

	onExit := func() {
		slog.Info("system tray exited")
	}

	systray.Run(onReady, onExit)
}

// Quit requests the system tray loop to terminate.
func Quit() {
	systray.Quit()
}
