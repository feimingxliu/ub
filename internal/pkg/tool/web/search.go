package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type searchRequest struct {
	Query   string
	Recency int
	Domains []string
	Limit   int
}

type searchResult struct {
	Title     string
	URL       string
	Summary   string
	Published string
}

type searchBackend interface {
	Search(ctx context.Context, req searchRequest) ([]searchResult, error)
}

func searchBackendFor(opts Options, client *http.Client) searchBackend {
	return httpSearchBackend{opts: opts, client: client}
}

type httpSearchBackend struct {
	opts   Options
	client *http.Client
}

func (b httpSearchBackend) Search(ctx context.Context, req searchRequest) ([]searchResult, error) {
	switch b.opts.Provider {
	case "":
		return nil, fmt.Errorf("tools.web.provider is required for web_search (supported: searxng, brave, tavily, serpapi)")
	case "searxng":
		return b.searchSearXNG(ctx, req)
	case "brave":
		return b.searchBrave(ctx, req)
	case "tavily":
		return b.searchTavily(ctx, req)
	case "serpapi":
		return b.searchSerpAPI(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported tools.web.provider %q (supported: searxng, brave, tavily, serpapi)", b.opts.Provider)
	}
}

func (b httpSearchBackend) searchSearXNG(ctx context.Context, req searchRequest) ([]searchResult, error) {
	if strings.TrimSpace(b.opts.BaseURL) == "" {
		return nil, fmt.Errorf("tools.web.base_url is required for searxng")
	}
	u, err := url.Parse(b.opts.BaseURL + "/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", queryWithDomains(req.Query, req.Domains))
	q.Set("format", "json")
	if req.Recency > 0 {
		q.Set("time_range", searxngTimeRange(req.Recency))
	}
	u.RawQuery = q.Encode()
	httpReq, err := newRequest(ctx, http.MethodGet, u.String(), nil, b.opts.UserAgent)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
			Published     string `json:"published"`
		} `json:"results"`
	}
	if err := b.doJSON(httpReq, &payload); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		out = append(out, searchResult{
			Title:     item.Title,
			URL:       item.URL,
			Summary:   item.Content,
			Published: fallback(item.PublishedDate, item.Published),
		})
	}
	return out, nil
}

func (b httpSearchBackend) searchBrave(ctx context.Context, req searchRequest) ([]searchResult, error) {
	if strings.TrimSpace(b.opts.APIKey) == "" {
		return nil, fmt.Errorf("tools.web.api_key is required for brave")
	}
	base := fallback(b.opts.BaseURL, "https://api.search.brave.com/res/v1/web/search")
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", queryWithDomains(req.Query, req.Domains))
	q.Set("count", fmt.Sprintf("%d", req.Limit))
	if req.Recency > 0 {
		q.Set("freshness", braveFreshness(req.Recency))
	}
	u.RawQuery = q.Encode()
	httpReq, err := newRequest(ctx, http.MethodGet, u.String(), nil, b.opts.UserAgent)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Subscription-Token", b.opts.APIKey)
	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := b.doJSON(httpReq, &payload); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(payload.Web.Results))
	for _, item := range payload.Web.Results {
		out = append(out, searchResult{Title: item.Title, URL: item.URL, Summary: item.Description, Published: item.Age})
	}
	return out, nil
}

func (b httpSearchBackend) searchTavily(ctx context.Context, req searchRequest) ([]searchResult, error) {
	if strings.TrimSpace(b.opts.APIKey) == "" {
		return nil, fmt.Errorf("tools.web.api_key is required for tavily")
	}
	base := fallback(b.opts.BaseURL, "https://api.tavily.com/search")
	body := map[string]any{
		"api_key":        b.opts.APIKey,
		"query":          queryWithDomains(req.Query, req.Domains),
		"max_results":    req.Limit,
		"include_answer": false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := newRequest(ctx, http.MethodPost, base, bytes.NewReader(raw), b.opts.UserAgent)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var payload struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			Published     string `json:"published"`
			PublishedAt   string `json:"published_at"`
			PublishedDate string `json:"published_date"`
		} `json:"results"`
	}
	if err := b.doJSON(httpReq, &payload); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		published := fallback(item.PublishedDate, fallback(item.PublishedAt, item.Published))
		out = append(out, searchResult{Title: item.Title, URL: item.URL, Summary: item.Content, Published: published})
	}
	return out, nil
}

func (b httpSearchBackend) searchSerpAPI(ctx context.Context, req searchRequest) ([]searchResult, error) {
	if strings.TrimSpace(b.opts.APIKey) == "" {
		return nil, fmt.Errorf("tools.web.api_key is required for serpapi")
	}
	base := fallback(b.opts.BaseURL, "https://serpapi.com/search.json")
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("engine", "google")
	q.Set("q", queryWithDomains(req.Query, req.Domains))
	q.Set("api_key", b.opts.APIKey)
	q.Set("num", fmt.Sprintf("%d", req.Limit))
	u.RawQuery = q.Encode()
	httpReq, err := newRequest(ctx, http.MethodGet, u.String(), nil, b.opts.UserAgent)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
			Date    string `json:"date"`
		} `json:"organic_results"`
	}
	if err := b.doJSON(httpReq, &payload); err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(payload.Organic))
	for _, item := range payload.Organic {
		out = append(out, searchResult{Title: item.Title, URL: item.Link, Summary: item.Snippet, Published: item.Date})
	}
	return out, nil
}

func (b httpSearchBackend) doJSON(req *http.Request, out any) error {
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", req.URL.String(), resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}

func queryWithDomains(query string, domains []string) string {
	query = strings.TrimSpace(query)
	for _, domain := range domains {
		if domain = normalizeDomain(domain); domain != "" {
			query += " site:" + domain
		}
	}
	return strings.TrimSpace(query)
}

func searxngTimeRange(days int) string {
	switch {
	case days <= 1:
		return "day"
	case days <= 7:
		return "week"
	case days <= 31:
		return "month"
	default:
		return "year"
	}
}

func braveFreshness(days int) string {
	switch {
	case days <= 1:
		return "pd"
	case days <= 7:
		return "pw"
	case days <= 31:
		return "pm"
	default:
		return "py"
	}
}
