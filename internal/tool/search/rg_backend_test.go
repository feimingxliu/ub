package search

import (
	"context"
	"reflect"
	"testing"
)

type fakeRunner struct {
	gotName string
	gotArgs []string
	gotDir  string
	out     []byte
	err     error
}

func (f *fakeRunner) output(_ context.Context, root, name string, args ...string) ([]byte, error) {
	f.gotDir = root
	f.gotName = name
	f.gotArgs = args
	return f.out, f.err
}

func TestRgBackend_BuildsArgs(t *testing.T) {
	f := &fakeRunner{out: []byte("a.go:1:hello\n")}
	be := &rgBackend{runner: f}
	root := t.TempDir()
	hits, err := be.run(context.Background(), grepOpts{
		rawPattern: "hello",
		root:       root,
		searchPath: root,
		include:    "*.go",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wantArgs := []string{
		"--line-number", "--no-heading", "--color=never", "--no-messages",
		"-g", "*.go",
		"hello", root,
	}
	if f.gotName != "rg" {
		t.Errorf("command name = %q want rg", f.gotName)
	}
	if !reflect.DeepEqual(f.gotArgs, wantArgs) {
		t.Errorf("args = %v\nwant   %v", f.gotArgs, wantArgs)
	}
	if f.gotDir != root {
		t.Errorf("cwd = %q want %q", f.gotDir, root)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %+v", hits)
	}
	if hits[0].Path != "a.go" || hits[0].Line != 1 || hits[0].Text != "hello" {
		t.Errorf("hit mismatch: %+v", hits[0])
	}
}

func TestRgBackend_NoIncludeFlag(t *testing.T) {
	f := &fakeRunner{out: nil}
	be := &rgBackend{runner: f}
	root := t.TempDir()
	if _, err := be.run(context.Background(), grepOpts{
		rawPattern: "x",
		root:       root,
		searchPath: root,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, a := range f.gotArgs {
		if a == "-g" {
			t.Fatalf("did not expect -g without include; args=%v", f.gotArgs)
		}
	}
}

func TestParseRipgrepOutput_MatchWithColons(t *testing.T) {
	root := "/tmp/ws"
	out := []byte("a/b.go:42:foo:bar:baz\n")
	hits, err := parseRipgrepOutput(out, root)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Path != "a/b.go" || hits[0].Line != 42 || hits[0].Text != "foo:bar:baz" {
		t.Errorf("hit mismatch: %+v", hits[0])
	}
}
