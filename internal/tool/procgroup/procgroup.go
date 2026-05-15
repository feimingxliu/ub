// Package procgroup encapsulates POSIX process-group creation and
// signalling for tool implementations that need to reliably kill an
// entire subprocess tree (the child plus any grandchildren it spawns).
//
// Set configures cmd to become its own process-group leader on Start.
// Kill signals every process in that group. On Windows both calls
// return a not-supported error so the rest of the package compiles
// cross-platform; V1 callers (bash, job) gate on runtime.GOOS first.
package procgroup
