// Package web implements ub's built-in audited web_search and web_fetch tools.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/invopop/jsonschema"
	"golang.org/x/net/html"

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

func newHTTPClient(opts Options) *http.Client {
	policy := domainPolicy{allow: opts.AllowDomains, deny: opts.DenyDomains}
	return &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			if err := validateHTTPURL(req.Context(), req.URL, opts.AllowPrivateNetwork); err != nil {
				return err
			}
			if err := policy.checkHost(req.URL.Host); err != nil {
				return err
			}
			return nil
		},
	}
}

type domainPolicy struct {
	allow []string
	deny  []string
}

func (p domainPolicy) checkHost(host string) error {
	host = normalizeHost(host)
	if host == "" {
		return fmt.Errorf("empty host")
	}
	for _, denied := range p.deny {
		if domainMatches(denied, host) {
			return fmt.Errorf("domain %q is denied by tools.web.deny_domains", host)
		}
	}
	if len(p.allow) == 0 {
		return nil
	}
	for _, allowed := range p.allow {
		if domainMatches(allowed, host) {
			return nil
		}
	}
	return fmt.Errorf("domain %q is not allowed by tools.web.allow_domains", host)
}

func (p domainPolicy) checkDomain(domain string) error {
	return p.checkHost(domain)
}

func normalizeDomainList(in []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, item := range in {
		item = normalizeDomain(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeDomain(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "*.")
	raw = strings.Trim(raw, "/")
	if h, _, err := net.SplitHostPort(raw); err == nil {
		raw = h
	}
	return strings.Trim(raw, "[]")
}

func normalizeHost(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if h, _, err := net.SplitHostPort(raw); err == nil {
		raw = h
	}
	return strings.Trim(raw, "[]")
}

func domainMatches(pattern, host string) bool {
	pattern = normalizeDomain(pattern)
	host = normalizeHost(host)
	if pattern == "" || host == "" {
		return false
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

func validateHTTPURL(ctx context.Context, u *url.URL, allowPrivate bool) error {
	if u == nil {
		return fmt.Errorf("url is required")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q; use http or https", u.Scheme)
	}
	host := normalizeHost(u.Host)
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	if allowPrivate {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if privateAddr(addr) {
			return fmt.Errorf("refusing private or local network address %s", host)
		}
		return nil
	}
	resolver := net.DefaultResolver
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			return fmt.Errorf("resolve %s: invalid IP %s", host, ip.IP.String())
		}
		if privateAddr(addr.Unmap()) {
			return fmt.Errorf("refusing private or local network address %s (%s)", host, addr.String())
		}
	}
	return nil
}

func privateAddr(addr netip.Addr) bool {
	return addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified() ||
		addr.IsMulticast()
}

func newRequest(ctx context.Context, method, rawURL string, body io.Reader, userAgent string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/plain,application/pdf;q=0.8,*/*;q=0.2")
	return req, nil
}

func readLimited(r io.Reader, max int64) ([]byte, bool, error) {
	if max <= 0 {
		max = defaultMaxFetchSize
	}
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	data := b.Bytes()
	if n > max {
		return data[:max], true, nil
	}
	return data, false, nil
}

func clampPositive(value, fallback, maxValue int) int {
	if value <= 0 {
		value = fallback
	}
	if maxValue > 0 && value > maxValue {
		value = maxValue
	}
	return value
}

func validPrefix(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

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
	if err := tool.UnmarshalArgs(raw, &args); err != nil {
		return tool.Result{}, fmt.Errorf("web_search: invalid args: %w", err)
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

type fetchArgs struct {
	URL      string      `json:"url" jsonschema:"required,description=HTTP or HTTPS URL to fetch. file:, localhost, and private network targets are blocked unless tools.web.allow_private_network is enabled."`
	MaxChars tool.IntArg `json:"max_chars,omitempty" jsonschema:"description=Maximum extracted characters returned inline before spillover. Defaults to 12000 and caps at 50000."`
}

type fetchTool struct {
	opts   Options
	client *http.Client
	policy domainPolicy
	schema *jsonschema.Schema
}

func newFetchTool(opts Options, client *http.Client) *fetchTool {
	return &fetchTool{
		opts:   opts,
		client: client,
		policy: domainPolicy{allow: opts.AllowDomains, deny: opts.DenyDomains},
		schema: jsonschema.Reflect(&fetchArgs{}),
	}
}

func (t *fetchTool) Name() string { return "web_fetch" }

func (t *fetchTool) Description() string {
	return "Fetch and extract text from one HTTP(S) page or PDF. Blocks local/private network targets by default, observes timeout/redirect/content-size limits, and returns source metadata plus extracted text with spillover for large pages."
}

func (t *fetchTool) Schema() *jsonschema.Schema { return t.schema }
func (t *fetchTool) Risk() tool.Risk            { return tool.RiskNetwork }

func (t *fetchTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args fetchArgs
	if err := tool.UnmarshalArgs(raw, &args); err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: invalid args: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: invalid url: %w", err)
	}
	if err := validateHTTPURL(ctx, u, t.opts.AllowPrivateNetwork); err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: %w", err)
	}
	if err := t.policy.checkHost(u.Host); err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: %w", err)
	}
	if allowed, err := t.allowedByRobots(ctx, u); err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: robots.txt: %w", err)
	} else if !allowed {
		return tool.Result{}, fmt.Errorf("web_fetch: blocked by robots.txt for %s", u.Host)
	}

	req, err := newRequest(ctx, http.MethodGet, u.String(), nil, t.opts.UserAgent)
	if err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: build request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: request %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tool.Result{}, fmt.Errorf("web_fetch: %s returned HTTP %d", u.String(), resp.StatusCode)
	}
	data, sizeTruncated, err := readLimited(resp.Body, t.opts.MaxFetchBytes)
	if err != nil {
		return tool.Result{}, fmt.Errorf("web_fetch: read response: %w", err)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	text, parser := extractText(data, contentType)
	if strings.TrimSpace(text) == "" {
		return tool.Result{}, fmt.Errorf("web_fetch: no extractable text from content type %q", fallback(contentType, "unknown"))
	}
	text = compactWhitespace(text)
	maxChars := clampPositive(int(args.MaxChars), defaultFetchChars, maxFetchChars)
	full := formatFetchResult(resp.Request.URL.String(), u.String(), contentType, parser, resp.StatusCode, len(data), sizeTruncated, text)
	visible := full
	if len([]rune(visible)) > maxChars {
		visible = validPrefix(visible, maxChars) + fmt.Sprintf("\n... [web_fetch output truncated: max_chars=%d]", maxChars)
	}
	return tool.Result{
		Content:       visible,
		FullContent:   full,
		Truncated:     visible != full || sizeTruncated,
		OriginalBytes: len([]byte(full)),
		Metadata: map[string]string{
			"url":          u.String(),
			"final_url":    resp.Request.URL.String(),
			"content_type": contentType,
			"parser":       parser,
			"source_bytes": fmt.Sprintf("%d", len(data)),
		},
	}, nil
}

func (t *fetchTool) allowedByRobots(ctx context.Context, target *url.URL) (bool, error) {
	robotsURL := *target
	robotsURL.Path = "/robots.txt"
	robotsURL.RawQuery = ""
	robotsURL.Fragment = ""
	req, err := newRequest(ctx, http.MethodGet, robotsURL.String(), nil, t.opts.UserAgent)
	if err != nil {
		return false, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return true, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return true, nil
	}
	data, _, err := readLimited(resp.Body, 128*1024)
	if err != nil {
		return false, err
	}
	return robotsAllows(string(data), t.opts.UserAgent, target.EscapedPath()), nil
}

func robotsAllows(body, userAgent, requestPath string) bool {
	requestPath = path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if requestPath == "." {
		requestPath = "/"
	}
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	var active bool
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "user-agent":
			v := strings.ToLower(value)
			active = v == "*" || (userAgent != "" && strings.Contains(userAgent, v))
		case "disallow":
			if !active || value == "" {
				continue
			}
			disallow := path.Clean("/" + strings.TrimPrefix(value, "/"))
			if requestPath == disallow || strings.HasPrefix(requestPath, strings.TrimRight(disallow, "/")+"/") {
				return false
			}
		}
	}
	return true
}

func formatFetchResult(finalURL, requestedURL, contentType, parser string, status, bytesRead int, sizeTruncated bool, text string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "source: %s\n", finalURL)
	if requestedURL != finalURL {
		fmt.Fprintf(&b, "requested_url: %s\n", requestedURL)
	}
	fmt.Fprintf(&b, "status: %d\n", status)
	fmt.Fprintf(&b, "content_type: %s\n", fallback(contentType, "unknown"))
	fmt.Fprintf(&b, "parser: %s\n", parser)
	fmt.Fprintf(&b, "source_bytes: %d\n", bytesRead)
	if sizeTruncated {
		b.WriteString("source_truncated: true\n")
	}
	b.WriteString("\nsummary:\n")
	b.WriteString(validPrefix(text, 1200))
	if len([]rune(text)) > 1200 {
		b.WriteString("...")
	}
	b.WriteString("\n\nextracted_text:\n")
	b.WriteString(text)
	return b.String()
}

func extractText(data []byte, contentType string) (string, string) {
	switch {
	case strings.Contains(contentType, "html") || looksLikeHTML(data):
		return extractHTMLText(data), "html"
	case strings.Contains(contentType, "pdf") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF")):
		return extractPDFText(data), "pdf"
	default:
		if utf8.Valid(data) {
			return string(data), "text"
		}
		return string(bytes.ToValidUTF8(data, []byte(" "))), "text"
	}
}

func looksLikeHTML(data []byte) bool {
	prefix := strings.ToLower(string(bytes.TrimSpace(data[:min(len(data), 512)])))
	return strings.Contains(prefix, "<html") || strings.Contains(prefix, "<!doctype html")
}

func extractHTMLText(data []byte) string {
	z := html.NewTokenizer(bytes.NewReader(data))
	var b strings.Builder
	skipDepth := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return b.String()
			}
			return b.String()
		case html.StartTagToken:
			name, _ := z.TagName()
			switch strings.ToLower(string(name)) {
			case "script", "style", "noscript", "svg":
				skipDepth++
			case "p", "br", "div", "section", "article", "li", "tr", "h1", "h2", "h3", "h4":
				b.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch strings.ToLower(string(name)) {
			case "script", "style", "noscript", "svg":
				if skipDepth > 0 {
					skipDepth--
				}
			case "p", "div", "section", "article", "li", "tr":
				b.WriteByte('\n')
			}
		case html.TextToken:
			if skipDepth == 0 {
				text := strings.TrimSpace(string(z.Text()))
				if text != "" {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(text)
				}
			}
		}
	}
}

func extractPDFText(data []byte) string {
	var parts []string
	for i := 0; i < len(data); i++ {
		if data[i] != '(' {
			continue
		}
		i++
		var b strings.Builder
		escaped := false
		for ; i < len(data); i++ {
			c := data[i]
			if escaped {
				switch c {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(c)
				}
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == ')' {
				break
			}
			if c >= 32 || c == '\n' || c == '\t' {
				b.WriteByte(c)
			}
		}
		text := strings.TrimSpace(b.String())
		if len(text) >= 3 && printableRatio(text) > 0.75 {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func printableRatio(text string) float64 {
	if text == "" {
		return 0
	}
	printable := 0
	total := 0
	for _, r := range text {
		total++
		if r == '\n' || r == '\t' || r >= 32 {
			printable++
		}
	}
	return float64(printable) / float64(total)
}

func compactWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
