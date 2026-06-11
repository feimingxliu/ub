package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type fakeBackend struct {
	gotOpts grepOpts
	hits    []grepHit
	err     error
}

func (f *fakeBackend) run(_ context.Context, opts grepOpts) ([]grepHit, error) {
	f.gotOpts = opts
	return f.hits, f.err
}

func swapBackend(t *testing.T, fb backend) {
	t.Helper()
	orig := newBackend
	t.Cleanup(func() { newBackend = orig })
	newBackend = func() backend { return fb }
}

func execGrep(t *testing.T, g *grepTool, args grepArgs) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return g.Execute(context.Background(), raw)
}

func TestGrep_InvalidRegex(t *testing.T) {
	g := newGrepTool(t.TempDir())
	if _, err := execGrep(t, g, grepArgs{Pattern: "("}); err == nil {
		t.Fatalf("expected regex error")
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	g := newGrepTool(t.TempDir())
	if _, err := execGrep(t, g, grepArgs{}); err == nil {
		t.Fatalf("expected required-pattern error")
	}
}

func TestGrep_DefaultPathIsRoot(t *testing.T) {
	root := t.TempDir()
	fb := &fakeBackend{}
	swapBackend(t, fb)
	g := newGrepTool(root)
	if _, err := execGrep(t, g, grepArgs{Pattern: "hello"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fb.gotOpts.searchPath != root {
		t.Fatalf("searchPath = %q want root %q", fb.gotOpts.searchPath, root)
	}
}

func TestGrep_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	g := newGrepTool(root)
	if _, err := execGrep(t, g, grepArgs{Pattern: ".", Path: "../"}); err == nil {
		t.Fatalf("expected sandbox error")
	} else if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("expected workspace error, got: %v", err)
	}
}

func TestGrep_NoMatchEmptyContent(t *testing.T) {
	root := t.TempDir()
	swapBackend(t, &fakeBackend{})
	g := newGrepTool(root)
	res, err := execGrep(t, g, grepArgs{Pattern: "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Content != "" || res.IsError {
		t.Fatalf("expected empty Content & IsError=false, got %+v", res)
	}
}

func TestGrep_SortsByPathThenLine(t *testing.T) {
	root := t.TempDir()
	fb := &fakeBackend{
		hits: []grepHit{
			{Path: "b.txt", Line: 1, Text: "x"},
			{Path: "a.txt", Line: 2, Text: "x"},
			{Path: "a.txt", Line: 1, Text: "x"},
		},
	}
	swapBackend(t, fb)
	g := newGrepTool(root)
	res, err := execGrep(t, g, grepArgs{Pattern: "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "a.txt:1:x\na.txt:2:x\nb.txt:1:x"
	if res.Content != want {
		t.Fatalf("output mismatch:\n got %q\nwant %q", res.Content, want)
	}
}

func TestGrep_PassesIncludeAndPath(t *testing.T) {
	root := t.TempDir()
	fb := &fakeBackend{}
	swapBackend(t, fb)
	g := newGrepTool(root)
	if _, err := execGrep(t, g, grepArgs{Pattern: "hello", Path: ".", Include: "*.go"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fb.gotOpts.include != "*.go" {
		t.Fatalf("include = %q", fb.gotOpts.include)
	}
	if fb.gotOpts.rawPattern != "hello" {
		t.Fatalf("rawPattern = %q", fb.gotOpts.rawPattern)
	}
}

func TestGrep_DefaultBackendIsGo(t *testing.T) {
	if be, ok := newBackend().(*goBackend); !ok || be == nil {
		t.Fatalf("default backend MUST be *goBackend, got %T", newBackend())
	}
}
