package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return tl.Execute(context.Background(), raw)
}

func TestRead_WholeFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "alpha\nbeta\ngamma\n")
	r := newReadTool(root)
	res, err := execTool(t, r, readArgs{Path: "a.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "1\talpha\n2\tbeta\n3\tgamma"
	if res.Content != want {
		t.Fatalf("content mismatch:\n got %q\nwant %q", res.Content, want)
	}
}

func TestRead_OffsetAndLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "b.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	r := newReadTool(root)
	res, err := execTool(t, r, readArgs{Path: "b.txt", Offset: 3, Limit: 2})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "3\t3\n4\t4"
	if res.Content != want {
		t.Fatalf("content mismatch:\n got %q\nwant %q", res.Content, want)
	}
}

func TestRead_LargeFileTruncated(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 2100; i++ {
		b.WriteString("x\n")
	}
	writeFile(t, root, "big.txt", b.String())
	r := newReadTool(root)
	res, err := execTool(t, r, readArgs{Path: "big.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(res.Content, "... (truncated, use offset/limit)") {
		t.Fatalf("expected truncation marker, got tail: %q", tail(res.Content, 80))
	}
	if strings.Count(res.Content, "\n") < 1999 {
		t.Fatalf("expected ~2000 lines in output, got %d newlines", strings.Count(res.Content, "\n"))
	}
}

func TestRead_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	r := newReadTool(root)
	if _, err := execTool(t, r, readArgs{Path: "../escape"}); err == nil {
		t.Fatalf("expected sandbox error")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
