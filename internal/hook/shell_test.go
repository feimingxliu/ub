package hook

import (
	"os/exec"
	"runtime"
)

// shellCommand returns an argv slice that runs the given shell script through
// /bin/sh. The hook integration tests use Unix shell syntax and are skipped on
// platforms without /bin/sh.
func shellCommand(script string) []string {
	return []string{"/bin/sh", "-c", script}
}

// hasShell reports whether the Unix shell used by these integration tests is
// available.
func hasShell() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := exec.LookPath("/bin/sh")
	return err == nil
}
