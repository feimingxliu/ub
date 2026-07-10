package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

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
	if err := tool.DecodeArgs("web_fetch", raw, &args); err != nil {
		return tool.Result{}, err
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
	if prefix, truncated := runePrefix(visible, maxChars); truncated {
		visible = prefix + fmt.Sprintf("\n... [web_fetch output truncated: max_chars=%d]", maxChars)
	}
	return tool.Result{
		Content:       visible,
		FullContent:   full,
		Truncated:     visible != full || sizeTruncated,
		OriginalBytes: len(full),
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
	b.Grow(len(text) + len(finalURL) + len(requestedURL) + 256)
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
	if prefix, truncated := runePrefix(text, 1200); truncated {
		b.WriteString(prefix)
		b.WriteString("...")
	} else {
		b.WriteString(prefix)
	}
	b.WriteString("\n\nextracted_text:\n")
	b.WriteString(text)
	return b.String()
}
