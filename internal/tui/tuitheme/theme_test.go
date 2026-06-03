package tuitheme

import "testing"

func TestForThemeSelectsMarkdownStyle(t *testing.T) {
	cases := []struct {
		name  string
		theme string
		want  string
	}{
		{name: "default", theme: "", want: "dark"},
		{name: "light", theme: "light", want: "light"},
		{name: "plain alias", theme: "plain", want: "notty"},
		{name: "tokyo alias", theme: "tokyo_night", want: "tokyo-night"},
		{name: "unknown", theme: "unsupported", want: "dark"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ForTheme(tc.theme).Markdown.StyleName; got != tc.want {
				t.Fatalf("ForTheme(%q).Markdown.StyleName = %q, want %q", tc.theme, got, tc.want)
			}
		})
	}
}

func TestPlainThemeUsesNoTTYMarkdown(t *testing.T) {
	if got := Plain().Markdown.StyleName; got != "notty" {
		t.Fatalf("Plain().Markdown.StyleName = %q, want notty", got)
	}
}
