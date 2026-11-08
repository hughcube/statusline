//go:build windows

package pathx

import (
	"os/exec"
	"syscall"
)

// DetachCmd configures cmd to start as a detached background worker with no
// console window, preventing Windows from creating console handles or stalling
// the calling terminal.
func DetachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
