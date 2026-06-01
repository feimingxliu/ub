// Package procgroup encapsulates POSIX process-group creation and
// signalling for tool implementations that need to reliably kill an
// entire subprocess tree (the child plus any grandchildren it spawns).
//
// Set configures cmd to become its own process-group leader on Start.
// Kill signals every process in that group. On Windows Set is a no-op
// and Kill returns a not-supported error; callers should fall back to
// cmd.Process.Kill() on Windows (which terminates only the direct
// child — acceptable for most tool use cases).
package procgroup
