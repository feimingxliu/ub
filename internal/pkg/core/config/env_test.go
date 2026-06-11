package config

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	t.Setenv("UB_TEST_KEY", "sk-abc")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple replacement",
			in:   "api_key: ${UB_TEST_KEY}",
			want: "api_key: sk-abc",
		},
		{
			name: "default fallback",
			in:   "base_url: ${UB_MISSING_KEY:-http://localhost:11434}",
			want: "base_url: http://localhost:11434",
		},
		{
			name: "dollar escape",
			in:   `prompt: "cost $$5 per call"`,
			want: `prompt: "cost $5 per call"`,
		},
		{
			name: "unsupported variable name is literal",
			in:   "api_key: ${lower_case}",
			want: "api_key: ${lower_case}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(Expand([]byte(tt.in))); got != tt.want {
				t.Fatalf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandWarnsForMissingVariable(t *testing.T) {
	unsetenv(t, "UB_MISSING_WARN_KEY")

	old := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	got := string(Expand([]byte("api_key: ${UB_MISSING_WARN_KEY}")))
	if got != "api_key: " {
		t.Fatalf("unexpected expansion %q", got)
	}
	if !strings.Contains(logs.String(), "UB_MISSING_WARN_KEY") {
		t.Fatalf("missing warning log, got %q", logs.String())
	}
}

func unsetenv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
