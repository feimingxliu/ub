//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd so that the child becomes the leader
// of a fresh process group. killProcessGroup can then signal the
// whole group, catching any grandchildren the command may spawn.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends sig to the entire process group whose
// leader has the given pid (achieved by passing -pid to syscall.Kill).
func killProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, sig)
}
