package diffview

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestViewEmptyDiff(t *testing.T) {
	if got := New(nil).View(); !strings.Contains(got, "No diff preview") {
		t.Fatalf("View() = %q, want empty state", got)
	}
}

func TestViewSingleFile(t *testing.T) {
	model := New([]tool.FileDiff{{
		Path:        "main.go",
		Kind:        tool.KindModify,
		UnifiedDiff: "--- a/main.go\n+++ b/main.go\n@@\n-old\n+new\n",
	}})
	view := model.View()
	for _, want := range []string{"main.go", "modify", "old", "new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestNavigationWraps(t *testing.T) {
	model := New([]tool.FileDiff{
		{Path: "a.go", UnifiedDiff: "a"},
		{Path: "b.py", UnifiedDiff: "b"},
	})
	if got := model.SelectedPath(); got != "a.go" {
		t.Fatalf("selected = %q, want a.go", got)
	}
	model = model.Next()
	if got := model.SelectedPath(); got != "b.py" {
		t.Fatalf("selected = %q, want b.py", got)
	}
	model = model.Next()
	if got := model.SelectedPath(); got != "a.go" {
		t.Fatalf("selected = %q, want wrapped a.go", got)
	}
	model = model.Prev()
	if got := model.SelectedPath(); got != "b.py" {
		t.Fatalf("selected = %q, want wrapped b.py", got)
	}
}

func TestCommonLanguageHighlightDoesNotPanic(t *testing.T) {
	for _, file := range []tool.FileDiff{
		{Path: "main.go", Kind: tool.KindModify, UnifiedDiff: "package main\nfunc main() {}\n"},
		{Path: "script.py", Kind: tool.KindModify, UnifiedDiff: "print('hi')\n"},
		{Path: "app.ts", Kind: tool.KindModify, UnifiedDiff: "const x: number = 1\n"},
	} {
		t.Run(file.Path, func(t *testing.T) {
			view := New([]tool.FileDiff{file}).View()
			if !strings.Contains(view, file.Path) {
				t.Fatalf("view missing path:\n%s", view)
			}
		})
	}
}
