//go:build windows

package ui

import (
	"os/exec"
	"syscall"
)

func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000010 | 0x00000200, // CREATE_NEW_CONSOLE | CREATE_NEW_PROCESS_GROUP
	}
}
