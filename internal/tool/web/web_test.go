package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestWebSearchSearXNGFormatsProviderNeutralResults(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); !strings.Contains(got, "site:docs.python.org") {
			t.Fatalf("query = %q, want site restriction", got)
		}
		return jsonResponse(r, http.StatusOK, map[string]any{
			"results": []map[string]any{
				{
					"title":         "Python docs",
					"url":           "https://docs.python.org/3/library/http.html",
					"content":       "HTTP client docs",
					"publishedDate": "2026-06-01",
				},
				{
					"title":   "Filtered",
					"url":     "https://example.com/nope",
					"content": "should be filtered by requested domain",
				},
			},
		})
	})

	reg := tool.New()
	if err := Register(reg, Options{
		Enabled:             true,
		Provider:            "searxng",
		BaseURL:             "https://search.example.test",
		AllowPrivateNetwork: true,
		Client:              client,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{
		"query":   "http client",
		"domains": []string{"docs.python.org"},
		"limit":   5,
	})
	res, err := searchTool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("web_search: %v", err)
	}
	if !strings.Contains(res.Content, "Python docs") || !strings.Contains(res.Content, "https://docs.python.org/3/library/http.html") {
		t.Fatalf("search result missing expected source:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "example.com") {
		t.Fatalf("search result should filter requested domains:\n%s", res.Content)
	}
	if res.Metadata["provider"] != "searxng" || res.Metadata["result_count"] != "1" {
		t.Fatalf("metadata = %#v", res.Metadata)
	}
}

func TestWebSearchDefaultDuckDuckGoFormatsResultsWithoutAPIKey(t *testing.T) {
	targetURL := "https://go.dev/doc"
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/html" {
			t.Fatalf("path = %q, want /html", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); !strings.Contains(got, "site:go.dev") {
			t.Fatalf("query = %q, want site restriction", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "Mozilla/5.0") || !strings.Contains(got, "ub-web/1.0") {
			t.Fatalf("user agent = %q, want browser-compatible ub crawler UA", got)
		}
		return textResponse(r, http.StatusOK, "text/html", `<html><body>
			<div class="result">
				<a class="result__a" href="/l/?uddg=`+url.QueryEscape(targetURL)+`">Go Documentation</a>
				<a class="result__snippet">Official Go documentation.</a>
			</div>
			<div class="result">
				<a class="result__a" href="/l/?uddg=`+url.QueryEscape("https://example.com/nope")+`">Filtered</a>
				<a class="result__snippet">Filtered by requested domain.</a>
			</div>
		</body></html>`), nil
	})

	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, BaseURL: "https://duck.example.test/html/", AllowPrivateNetwork: true, Client: client}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{
		"query":   "docs",
		"domains": []string{"go.dev"},
	})
	res, err := searchTool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("web_search: %v", err)
	}
	if !strings.Contains(res.Content, "Go Documentation") || !strings.Contains(res.Content, targetURL) || !strings.Contains(res.Content, "Official Go documentation.") {
		t.Fatalf("search result missing expected DuckDuckGo source:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "example.com") {
		t.Fatalf("search result should filter requested domains:\n%s", res.Content)
	}
	if res.Metadata["provider"] != "duckduckgo" || res.Metadata["result_count"] != "1" {
		t.Fatalf("metadata = %#v", res.Metadata)
	}
}

func TestParseDuckDuckGoHTMLDecodesResultLinks(t *testing.T) {
	targetURL := "https://go.dev/doc?q=a+b"
	results := parseDuckDuckGoHTML([]byte(`<html><body>
		<div class="result">
			<a class="result__a" href="/l/?uddg=` + url.QueryEscape(targetURL) + `">Go <b>Docs</b></a>
			<div class="result__snippet">Official &amp; current docs.</div>
		</div>
	</body></html>`))
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1: %#v", len(results), results)
	}
	if results[0].URL != targetURL || results[0].Title != "Go Docs" || results[0].Summary != "Official & current docs." {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestWebSearchBlocksPrivateProviderEndpointByDefault(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request to blocked provider endpoint: %s", r.URL.String())
		return nil, nil
	})
	reg := tool.New()
	if err := Register(reg, Options{
		Enabled: true,
		BaseURL: "http://127.0.0.1/html/",
		Client:  client,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{"query": "latest go"})
	_, err := searchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "refusing private or local network address") {
		t.Fatalf("web_search error = %v, want private network block", err)
	}
}

func TestWebSearchAppliesDomainPolicyToProviderEndpoint(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request to denied provider endpoint: %s", r.URL.String())
		return nil, nil
	})
	reg := tool.New()
	if err := Register(reg, Options{
		Enabled:             true,
		Provider:            "searxng",
		BaseURL:             "https://search.example.test",
		AllowPrivateNetwork: true,
		DenyDomains:         []string{"search.example.test"},
		Client:              client,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{"query": "latest go"})
	_, err := searchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "denied by tools.web.deny_domains") {
		t.Fatalf("web_search error = %v, want provider domain block", err)
	}
}

func TestWebSearchAllowDomainsBlocksUnlistedProvider(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request to unlisted provider endpoint: %s", r.URL.String())
		return nil, nil
	})
	reg := tool.New()
	if err := Register(reg, Options{
		Enabled:             true,
		Provider:            "searxng",
		BaseURL:             "https://search.example.test",
		AllowPrivateNetwork: true,
		AllowDomains:        []string{"allowed.test"},
		Client:              client,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{"query": "latest go"})
	_, err := searchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "not allowed by tools.web.allow_domains") {
		t.Fatalf("web_search error = %v, want allow_domains block", err)
	}
}

func TestWebSearchUnsupportedProviderReportsClearError(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, Provider: "unknown"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{"query": "latest go"})
	_, err := searchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "unsupported tools.web.provider") || !strings.Contains(err.Error(), "duckduckgo") {
		t.Fatalf("web_search error = %v, want unsupported provider", err)
	}
}

func TestWebFetchExtractsHTMLAndMetadata(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return textResponse(r, http.StatusNotFound, "text/plain", "not found"), nil
		case "/page":
			return textResponse(r, http.StatusOK, "text/html; charset=utf-8", `<html><head><script>secret()</script></head><body><h1>Hello</h1><p>Docs page.</p></body></html>`), nil
		default:
			return textResponse(r, http.StatusNotFound, "text/plain", "not found"), nil
		}
	})

	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, AllowPrivateNetwork: true, Client: client}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, ok := reg.Get("web_fetch")
	if !ok {
		t.Fatal("web_fetch missing")
	}
	raw, _ := json.Marshal(map[string]any{"url": "https://docs.example.test/page", "max_chars": 2000})
	res, err := fetchTool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	if !strings.Contains(res.Content, "Hello Docs page.") || strings.Contains(res.Content, "secret()") {
		t.Fatalf("fetch content = %q", res.Content)
	}
	if res.Metadata["parser"] != "html" || res.Metadata["url"] != "https://docs.example.test/page" {
		t.Fatalf("metadata = %#v", res.Metadata)
	}
}

func TestWebFetchBlocksRobotsDisallow(t *testing.T) {
	client := fakeHTTPClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return textResponse(r, http.StatusOK, "text/plain", "User-agent: *\nDisallow: /private\n"), nil
		case "/private/page":
			return textResponse(r, http.StatusOK, "text/plain", "blocked"), nil
		default:
			return textResponse(r, http.StatusNotFound, "text/plain", "not found"), nil
		}
	})

	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, AllowPrivateNetwork: true, Client: client}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, _ := reg.Get("web_fetch")
	raw, _ := json.Marshal(map[string]any{"url": "https://docs.example.test/private/page"})
	_, err := fetchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "blocked by robots.txt") {
		t.Fatalf("web_fetch error = %v, want robots block", err)
	}
}

func TestWebFetchBlocksPrivateNetworkByDefault(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, Options{Enabled: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, _ := reg.Get("web_fetch")
	raw, _ := json.Marshal(map[string]any{"url": "http://127.0.0.1/page"})
	_, err := fetchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "refusing private or local network address") {
		t.Fatalf("web_fetch error = %v, want private network block", err)
	}
}

func TestWebFetchAppliesDomainPolicyToRedirects(t *testing.T) {
	opts := Options{
		Enabled:             true,
		AllowPrivateNetwork: true,
		AllowDomains:        []string{"redirect.example.test"},
		DenyDomains:         []string{"target.example.test"},
	}
	client := newHTTPClient(normalizeOptions(opts))
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/robots.txt":
			return textResponse(r, http.StatusNotFound, "text/plain", "not found"), nil
		case "/start":
			resp := textResponse(r, http.StatusFound, "text/plain", "")
			resp.Header.Set("Location", "https://target.example.test/page")
			return resp, nil
		case "/page":
			return textResponse(r, http.StatusOK, "text/plain", "should not be fetched"), nil
		default:
			return textResponse(r, http.StatusNotFound, "text/plain", "not found"), nil
		}
	})
	opts.Client = client

	reg := tool.New()
	if err := Register(reg, opts); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, _ := reg.Get("web_fetch")
	raw, _ := json.Marshal(map[string]any{"url": "https://redirect.example.test/start"})
	_, err := fetchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "denied by tools.web.deny_domains") {
		t.Fatalf("web_fetch error = %v, want redirect domain block", err)
	}
}

func TestRegisterDisabledSkipsWebTools(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, Options{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Fatal("web_search registered while disabled")
	}
	if _, ok := reg.Get("web_fetch"); ok {
		t.Fatal("web_fetch registered while disabled")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func textResponse(req *http.Request, status int, contentType, body string) *http.Response {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func jsonResponse(req *http.Request, status int, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return textResponse(req, status, "application/json", string(raw)), nil
}
