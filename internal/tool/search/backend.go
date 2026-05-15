package search

import (
	"context"
	"regexp"
)

// grepOpts is the resolved input fed to a backend. pattern is already
// validated (regexp.Compile succeeded); searchPath is an absolute path
// inside root; include is the raw glob from the caller (may be empty).
type grepOpts struct {
	pattern    *regexp.Regexp
	rawPattern string
	root       string
	searchPath string
	include    string
}

// grepHit is the backend-neutral result. Path is relative to root with
// POSIX separators; Line is 1-based; Text is the matched line content
// (already truncated by the backend if needed).
type grepHit struct {
	Path string
	Line int
	Text string
}

// backend is the polymorphic search engine. Implementations MUST return
// hits sorted-or-unsorted; the tool sorts globally before emitting.
type backend interface {
	run(ctx context.Context, opts grepOpts) ([]grepHit, error)
}

// newBackend is the package-level injection point. Tests overwrite it
// to swap in a fake or to force the rg backend. The default returns
// the pure-Go backend, regardless of whether rg is on PATH.
var newBackend = func() backend {
	return &goBackend{}
}
