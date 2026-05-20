package config

import "testing"

func TestNormalizeAnthropicBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"official no v1", "https://api.anthropic.com", "https://api.anthropic.com"},
		{"official with v1", "https://api.anthropic.com/v1", "https://api.anthropic.com"},
		{"official with v1 trailing slash", "https://api.anthropic.com/v1/", "https://api.anthropic.com"},
		{"gateway no v1", "http://172.17.120.11:9091/vibecoding", "http://172.17.120.11:9091/vibecoding"},
		{"gateway with v1", "http://172.17.120.11:9091/vibecoding/v1", "http://172.17.120.11:9091/vibecoding"},
		{"gateway trailing slash", "http://172.17.120.11:9091/vibecoding/", "http://172.17.120.11:9091/vibecoding"},
		{"keep v1 inside path", "https://example.com/v1/proxy", "https://example.com/v1/proxy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeAnthropicBaseURL(tc.in); got != tc.want {
				t.Fatalf("NormalizeAnthropicBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
