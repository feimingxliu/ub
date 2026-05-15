//go:build windows

package shell

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setProcessGroup is a no-op on Windows; the bash tool itself returns
// a not-supported error before getting here.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup returns an explicit error on Windows. V1 does not
// support the bash tool on Windows; we keep the symbol so the rest
// of the package compiles cross-platform.
func killProcessGroup(pid int, sig syscall.Signal) error {
	return fmt.Errorf("shell: process-group kill not supported on windows")
}
