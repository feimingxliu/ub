package search

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func runGo(t *testing.T, root, search, include, pattern string) []grepHit {
	t.Helper()
	re := regexp.MustCompile(pattern)
	hits, err := (&goBackend{}).run(context.Background(), grepOpts{
		pattern:    re,
		rawPattern: pattern,
		root:       root,
		searchPath: search,
		include:    include,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return hits
}

func TestGoBackend_HappyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "hello\nworld\n")
	writeFile(t, root, "sub/b.txt", "say hello\n")

	hits := runGo(t, root, root, "", "hello")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
}

func TestGoBackend_IncludeFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "hello\n")
	writeFile(t, root, "b.md", "hello\n")

	hits := runGo(t, root, root, "*.go", "hello")
	if len(hits) != 1 || hits[0].Path != "a.go" {
		t.Fatalf("expected only a.go, got %+v", hits)
	}
}

func TestGoBackend_SkipsBinary(t *testing.T) {
	root := t.TempDir()
	// First byte is NUL → binary
	if err := os.WriteFile(filepath.Join(root, "bin.dat"),
		append([]byte{0}, []byte("hello\n")...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeFile(t, root, "ok.txt", "hello\n")

	hits := runGo(t, root, root, "", "hello")
	for _, h := range hits {
		if h.Path == "bin.dat" {
			t.Fatalf("binary file leaked into results: %+v", hits)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
}

func TestGoBackend_TruncatesLongLines(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("a", maxLineLen+200) + "ZZZ"
	writeFile(t, root, "long.txt", long+"\n")

	hits := runGo(t, root, root, "", "ZZZ")
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if !strings.HasSuffix(hits[0].Text, " ...(truncated)") {
		t.Fatalf("expected truncation suffix, got tail %q", tail(hits[0].Text, 30))
	}
	// "ZZZ" itself is past 2048, so it MUST be dropped after truncation
	if strings.Contains(hits[0].Text, "ZZZ") {
		t.Fatalf("truncation kept content past cap: %q", tail(hits[0].Text, 30))
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
