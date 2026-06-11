package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func TestWebSearchSearXNGFormatsProviderNeutralResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); !strings.Contains(got, "site:docs.python.org") {
			t.Fatalf("query = %q, want site restriction", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
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
	}))
	defer srv.Close()

	reg := tool.New()
	if err := Register(reg, Options{
		Enabled:  true,
		Provider: "searxng",
		BaseURL:  srv.URL,
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

func TestWebSearchMissingProviderReportsClearError(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, Options{Enabled: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	searchTool, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	raw, _ := json.Marshal(map[string]any{"query": "latest go"})
	_, err := searchTool.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "tools.web.provider is required") {
		t.Fatalf("web_search error = %v, want missing provider", err)
	}
}

func TestWebFetchExtractsHTMLAndMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><script>secret()</script></head><body><h1>Hello</h1><p>Docs page.</p></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, AllowPrivateNetwork: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, ok := reg.Get("web_fetch")
	if !ok {
		t.Fatal("web_fetch missing")
	}
	raw, _ := json.Marshal(map[string]any{"url": srv.URL + "/page", "max_chars": 2000})
	res, err := fetchTool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	if !strings.Contains(res.Content, "Hello Docs page.") || strings.Contains(res.Content, "secret()") {
		t.Fatalf("fetch content = %q", res.Content)
	}
	if res.Metadata["parser"] != "html" || res.Metadata["url"] != srv.URL+"/page" {
		t.Fatalf("metadata = %#v", res.Metadata)
	}
}

func TestWebFetchBlocksRobotsDisallow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
		case "/private/page":
			_, _ = w.Write([]byte("blocked"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := tool.New()
	if err := Register(reg, Options{Enabled: true, AllowPrivateNetwork: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, _ := reg.Get("web_fetch")
	raw, _ := json.Marshal(map[string]any{"url": srv.URL + "/private/page"})
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
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/page":
			_, _ = w.Write([]byte("should not be fetched"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()
	targetHost := strings.TrimPrefix(target.URL, "http://")

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/start":
			http.Redirect(w, r, target.URL+"/page", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer redirector.Close()
	redirectHost := strings.TrimPrefix(redirector.URL, "http://")

	reg := tool.New()
	if err := Register(reg, Options{
		Enabled:             true,
		AllowPrivateNetwork: true,
		AllowDomains:        []string{redirectHost},
		DenyDomains:         []string{targetHost},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fetchTool, _ := reg.Get("web_fetch")
	raw, _ := json.Marshal(map[string]any{"url": redirector.URL + "/start"})
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
