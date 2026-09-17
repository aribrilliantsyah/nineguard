package ui

import (
	"os"
	"os/exec"
	"strings"
)

// SpawnDaemon attempts to spawn a detached background instance of NineGuard.
// If withTray is true, it spawns with --tray. Otherwise, it spawns with --daemon.
// If running via `go run`, it returns spawned=false so the caller can run in-process.
func SpawnDaemon(port string, withTray bool) (bool, int, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, 0, err
	}

	// Detect go run temporary binaries
	if strings.Contains(exe, "go-build") || strings.Contains(exe, "/tmp/") {
		return false, 0, nil
	}

	var args []string
	if withTray {
		args = []string{"--tray", "-p", port}
	} else {
		args = []string{"--daemon", "-p", port}
	}

	cmd := exec.Command(exe, args...)
	setDetached(cmd)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return false, 0, err
	}

	return true, cmd.Process.Pid, nil
}
