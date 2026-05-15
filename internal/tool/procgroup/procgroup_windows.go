//go:build windows

package procgroup

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Set is a no-op on Windows; callers gate on runtime.GOOS first.
func Set(cmd *exec.Cmd) {}

// Kill returns an explicit not-supported error on Windows.
func Kill(pid int, sig syscall.Signal) error {
	return fmt.Errorf("procgroup: process-group kill not supported on windows")
}
