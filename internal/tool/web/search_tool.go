package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type searchArgs struct {
	Query   string      `json:"query" jsonschema:"required,description=Search query. Add site-specific terms only when they are part of the user's request."`
	Recency tool.IntArg `json:"recency,omitempty" jsonschema:"description=Optional recency window in days."`
	Domains []string    `json:"domains,omitempty" jsonschema:"description=Optional domains to restrict results to, e.g. golang.org or docs.python.org."`
	Limit   tool.IntArg `json:"limit,omitempty" jsonschema:"description=Maximum number of search results. Defaults to 5 and caps at 20."`
}

type searchTool struct {
	opts    Options
	backend searchBackend
	policy  domainPolicy
	schema  *jsonschema.Schema
}

func newSearchTool(opts Options, backend searchBackend) *searchTool {
	return &searchTool{
		opts:    opts,
		backend: backend,
		policy:  domainPolicy{allow: opts.AllowDomains, deny: opts.DenyDomains},
		schema:  jsonschema.Reflect(&searchArgs{}),
	}
}

func (t *searchTool) Name() string { return "web_search" }

func (t *searchTool) Description() string {
	return "Search the web for current external information. Returns provider-neutral result titles, URLs, summaries, and dates; use web_fetch on selected URLs when the page content matters. Uses the zero-config duckduckgo provider by default when tools.web.enabled is true."
}

func (t *searchTool) Schema() *jsonschema.Schema { return t.schema }
func (t *searchTool) Risk() tool.Risk            { return tool.RiskNetwork }

func (t *searchTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args searchArgs
	if err := tool.DecodeArgs("web_search", raw, &args); err != nil {
		return tool.Result{}, err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return tool.Result{}, fmt.Errorf("web_search: query is required")
	}
	domains := normalizeDomainList(args.Domains)
	for _, domain := range domains {
		if err := t.policy.checkDomain(domain); err != nil {
			return tool.Result{}, fmt.Errorf("web_search: %w", err)
		}
	}
	req := searchRequest{
		Query:   args.Query,
		Recency: int(args.Recency),
		Domains: domains,
		Limit:   clampPositive(int(args.Limit), defaultSearchLimit, maxSearchLimit),
	}
	results, err := t.backend.Search(ctx, req)
	if err != nil {
		return tool.Result{}, fmt.Errorf("web_search: %w", err)
	}
	results = filterSearchResults(results, domains, t.policy)
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}
	content := formatSearchResults(t.opts.Provider, req, results)
	return tool.Result{
		Content: content,
		Metadata: map[string]string{
			"provider":     t.opts.Provider,
			"query":        req.Query,
			"domains":      strings.Join(req.Domains, ","),
			"result_count": fmt.Sprintf("%d", len(results)),
		},
	}, nil
}

func filterSearchResults(results []searchResult, domains []string, policy domainPolicy) []searchResult {
	out := make([]searchResult, 0, len(results))
	for _, res := range results {
		u, err := url.Parse(strings.TrimSpace(res.URL))
		if err != nil || u.Host == "" {
			continue
		}
		host := normalizeHost(u.Host)
		if err := policy.checkHost(host); err != nil {
			continue
		}
		if len(domains) > 0 {
			matched := false
			for _, domain := range domains {
				if domainMatches(domain, host) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, res)
	}
	return out
}

func formatSearchResults(provider string, req searchRequest, results []searchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "web_search provider=%s query=%q", provider, req.Query)
	if len(req.Domains) > 0 {
		fmt.Fprintf(&b, " domains=%s", strings.Join(req.Domains, ","))
	}
	if req.Recency > 0 {
		fmt.Fprintf(&b, " recency_days=%d", req.Recency)
	}
	fmt.Fprintf(&b, " results=%d\n", len(results))
	if len(results) == 0 {
		b.WriteString("No results returned.")
		return b.String()
	}
	for i, res := range results {
		fmt.Fprintf(&b, "\n%d. %s\n", i+1, fallback(res.Title, "(untitled)"))
		fmt.Fprintf(&b, "url: %s\n", res.URL)
		if strings.TrimSpace(res.Published) != "" {
			fmt.Fprintf(&b, "date: %s\n", res.Published)
		}
		if strings.TrimSpace(res.Summary) != "" {
			fmt.Fprintf(&b, "summary: %s\n", compactWhitespace(res.Summary))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
