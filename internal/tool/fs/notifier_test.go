package fs

import (
	"context"
	"path/filepath"
	"testing"
)

type recordingNotifier struct {
	paths []string
}

func (n *recordingNotifier) DidChangeFile(_ context.Context, absPath string) error {
	n.paths = append(n.paths, absPath)
	return nil
}

func TestWriteAndEditNotifyAfterMutation(t *testing.T) {
	root := t.TempDir()
	notifier := &recordingNotifier{}

	write := newWriteToolWithNotifier(root, notifier)
	if _, err := execTool(t, write, writeArgs{Path: "a.txt", Content: "hello\n"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	edit := newEditToolWithNotifier(root, notifier)
	if _, err := execTool(t, edit, editArgs{Path: "a.txt", Old: "hello", New: "hi"}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	want := filepath.Join(root, "a.txt")
	if len(notifier.paths) != 2 {
		t.Fatalf("notifications = %d, want 2: %#v", len(notifier.paths), notifier.paths)
	}
	for _, got := range notifier.paths {
		if got != want {
			t.Fatalf("notified path = %q, want %q", got, want)
		}
	}
}
