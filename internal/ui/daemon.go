package ui

import (
	"os"
	"os/exec"
	"strings"
)

// SpawnDaemon attempts to spawn a detached background instance of NineGuard with --tray.
// If running via `go run`, it returns spawned=false so the caller can run tray in-process.
func SpawnDaemon(port string) (bool, int, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, 0, err
	}

	// Detect go run temporary binaries
	if strings.Contains(exe, "go-build") || strings.Contains(exe, "/tmp/") {
		return false, 0, nil
	}

	cmd := exec.Command(exe, "--tray", "-p", port)
	setDetached(cmd)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return false, 0, err
	}

	return true, cmd.Process.Pid, nil
}
