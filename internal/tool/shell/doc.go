// Package shell implements the bash shell-command tool.
//
// The tool wraps a single execution of `/bin/sh -c <command>` with
// these guarantees:
//
//   - Workspace sandbox: cwd is resolved through tool.Resolve and MUST
//     fall under the workspace root passed to Register.
//   - Timeout: defaults to 120s; an explicit timeout_ms may override
//     it. On expiry the child's process group is signalled with
//     SIGTERM, then SIGKILL 2s later.
//   - Output capture: stdout and stderr are each capped at 32 KB in
//     the returned Result.Content; the true byte counts are reported
//     in a truncation footer when the cap is exceeded.
//   - No interactivity: stdin is redirected to /dev/null.
//
// The tool does NOT implement PreviewableTool (shell commands are
// opaque), does NOT enforce permission policy (that lives in the
// dispatcher / permission layer in I-20), and does NOT manage
// background jobs (I-19).
package shell

import "time"

const (
	// defaultTimeout is the timeout applied when the caller does not
	// pass an explicit timeout_ms.
	defaultTimeout = 120 * time.Second

	// streamCap is the per-stream byte cap applied to stdout and
	// stderr when assembling Result.Content. The full byte count is
	// still reported in the truncation footer.
	streamCap = 32 * 1024
)
