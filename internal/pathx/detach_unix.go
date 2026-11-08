//go:build !windows

package pathx

import (
	"os/exec"
	"syscall"
)

// DetachCmd configures cmd to start in a new process group.
func DetachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
