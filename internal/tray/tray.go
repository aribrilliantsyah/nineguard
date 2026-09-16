package tray

// Options configures the system tray integration.
type Options struct {
	Port            string
	ServerURL       string
	OnOpenDashboard func()
	OnQuit          func()
}
