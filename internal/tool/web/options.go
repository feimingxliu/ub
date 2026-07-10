package web

import (
	"net/http"
	"strings"
	"time"
)

// Options configures web_search and web_fetch.
type Options struct {
	Enabled             bool
	Provider            string
	APIKey              string
	BaseURL             string
	UserAgent           string
	Timeout             time.Duration
	MaxFetchBytes       int64
	AllowDomains        []string
	DenyDomains         []string
	AllowPrivateNetwork bool
	Client              *http.Client
}

func normalizeOptions(opts Options) Options {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxFetchBytes <= 0 {
		opts.MaxFetchBytes = defaultMaxFetchSize
	}
	if strings.TrimSpace(opts.UserAgent) == "" {
		opts.UserAgent = defaultUserAgent
	}
	opts.Provider = strings.ToLower(strings.TrimSpace(opts.Provider))
	if opts.Provider == "" {
		opts.Provider = "duckduckgo"
	}
	opts.BaseURL = strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	opts.AllowDomains = normalizeDomainList(opts.AllowDomains)
	opts.DenyDomains = normalizeDomainList(opts.DenyDomains)
	return opts
}
