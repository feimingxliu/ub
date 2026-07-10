package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
)

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
	prefix, _ := runePrefix(text, maxRunes)
	return prefix
}

func runePrefix(text string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return "", text != ""
	}
	count := 0
	for idx := range text {
		if count == maxRunes {
			return text[:idx], true
		}
		count++
	}
	return text, false
}
