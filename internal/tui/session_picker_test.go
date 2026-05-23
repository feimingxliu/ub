package tui

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

func TestSessionPickerFuzzyFilter(t *testing.T) {
	picker := newSessionPicker([]SessionInfo{
		{ID: "s1", Title: "Alpha Planning", Model: "fake/one", Current: true},
		{ID: "s2", Title: "Bug Fix", Model: "fake/two"},
		{ID: "s3", Title: "Release Notes", Model: "fake/three"},
	})

	for _, r := range "rln" {
		picker.appendRune(r)
	}

	if got := picker.selected().ID; got != "s3" {
		t.Fatalf("selected session = %q, want s3", got)
	}
	view := picker.view(100, tuitheme.Default())
	if !strings.Contains(view, "Release Notes") || strings.Contains(view, "Alpha Planning") {
		t.Fatalf("filtered view did not narrow sessions:\n%s", view)
	}

	picker.backspace()
	picker.backspace()
	picker.backspace()
	if got := len(picker.sessions); got != 3 {
		t.Fatalf("sessions after clearing query = %d, want 3", got)
	}
	if got := picker.selected().ID; got != "s1" {
		t.Fatalf("selected session after clearing = %q, want current s1", got)
	}
}

func TestSessionPickerShowsEmptyFilterResult(t *testing.T) {
	picker := newSessionPicker([]SessionInfo{
		{ID: "s1", Title: "Alpha Planning", Model: "fake/one"},
	})

	for _, r := range "zzz" {
		picker.appendRune(r)
	}

	if got := picker.selected().ID; got != "" {
		t.Fatalf("selected session = %q, want empty selection", got)
	}
	view := picker.view(100, tuitheme.Default())
	if !strings.Contains(view, "filter: zzz") || !strings.Contains(view, "no matching sessions") {
		t.Fatalf("empty filter view missing state:\n%s", view)
	}
}
