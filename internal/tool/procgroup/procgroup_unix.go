//go:build !windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// Set configures cmd so that the child becomes the leader of a fresh
// process group. Kill can then signal the whole group, catching any
// grandchildren the command may spawn.
func Set(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill sends sig to the entire process group whose leader has the
// given pid (achieved by passing -pid to syscall.Kill).
func Kill(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, sig)
}
