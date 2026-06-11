// Package job implements the long-running background process tools:
// job_run, job_output and job_kill.
//
// A single Manager (created by Register) owns all jobs for a workspace.
// Each job wraps a child process started via /bin/sh -c <command> in
// its own process group; stdout and stderr are captured into 32 KB ring
// buffers so output is always bounded regardless of how long the
// process runs or how chatty it is. job_kill signals the entire process
// group with SIGTERM, then SIGKILL after 2 seconds.
//
// Jobs are NOT persisted across process restarts; when ub exits, every
// child is reaped by the OS. Permission policy (allow/deny rules, mode
// gates) lives in the dispatcher/permission layer and is not enforced
// here.
package job

import "time"

const (
	// streamCap caps each captured stream (stdout and stderr) at 32 KB
	// per job. The full byte count is preserved separately.
	streamCap = 32 * 1024
	// killGrace is how long the manager waits after SIGTERM before
	// escalating to SIGKILL on a kill request.
	killGrace = 2 * time.Second
)
