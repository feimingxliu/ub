// Package web implements ub's built-in audited web_search and web_fetch tools.
package web

import (
	"fmt"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

const (
	defaultSearchLimit  = 5
	maxSearchLimit      = 20
	defaultFetchChars   = 12000
	maxFetchChars       = 50000
	defaultTimeout      = 15 * time.Second
	defaultMaxFetchSize = 2 * 1024 * 1024
	defaultUserAgent    = "Mozilla/5.0 (compatible; ub-web/1.0)"
)

// Register adds web_search and web_fetch when opts.Enabled is true.
func Register(reg *tool.Registry, opts Options) error {
	if reg == nil {
		return fmt.Errorf("web: nil registry")
	}
	if !opts.Enabled {
		return nil
	}
	opts = normalizeOptions(opts)
	client := opts.Client
	if client == nil {
		client = newHTTPClient(opts)
	}
	backend := searchBackendFor(opts, client)
	for _, t := range []tool.Tool{
		newSearchTool(opts, backend),
		newFetchTool(opts, client),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
