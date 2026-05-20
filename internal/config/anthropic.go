package config

import "strings"

// NormalizeAnthropicBaseURL returns a base URL suitable for the Anthropic SDK,
// which expects to append "v1/..." paths itself. Trailing slashes and a
// trailing "/v1" segment are stripped so that user values written either as
// "https://api.anthropic.com" or "https://api.anthropic.com/v1" both resolve
// to the same canonical form.
func NormalizeAnthropicBaseURL(raw string) string {
	url := strings.TrimRight(strings.TrimSpace(raw), "/")
	url = strings.TrimSuffix(url, "/v1")
	return strings.TrimRight(url, "/")
}
