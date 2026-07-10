package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub-minute", 30 * time.Second, "30s"},
		{"minutes", 90 * time.Second, "1m30s"},
		{"hours", 3700 * time.Second, "1h01m"},
		{"zero", 0, "0s"},
		{"negative", -5 * time.Second, "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatElapsed(tc.in); got != tc.want {
				t.Fatalf("formatElapsed(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRunIndicatorViewHiddenWhenIdle(t *testing.T) {
	m := Model{styles: tuitheme.Default()}
	m.running = false
	m.status.state = statusThinking
	if got := m.runIndicatorView(80); got != "" {
		t.Fatalf("expected empty indicator when not running, got %q", got)
	}
}

func TestRunIndicatorViewHiddenOnFailure(t *testing.T) {
	m := Model{styles: tuitheme.Default()}
	m.running = true
	m.status.state = "failed"
	if got := m.runIndicatorView(80); got != "" {
		t.Fatalf("expected empty indicator on failure state, got %q", got)
	}
}

func TestRunIndicatorViewShowsSpinnerAndElapsed(t *testing.T) {
	m := Model{styles: tuitheme.Default()}
	m.running = true
	m.status.state = statusThinking
	m.runStartedAt = time.Now().Add(-5 * time.Second)
	m.spinnerFrame = 2
	m.activitySummary = "Reading file model.go"

	got := m.runIndicatorView(80)
	if got == "" {
		t.Fatal("expected non-empty indicator")
	}
	if !strings.Contains(got, spinnerFrames[2]) {
		t.Fatalf("expected spinner frame %q in output, got %q", spinnerFrames[2], got)
	}
	if !strings.Contains(got, "Thinking") {
		t.Fatalf("expected state label Thinking in output, got %q", got)
	}
	if !strings.Contains(got, "5s") {
		t.Fatalf("expected elapsed 5s in output, got %q", got)
	}
	if !strings.Contains(got, "Reading file model.go") {
		t.Fatalf("expected activity summary in output, got %q", got)
	}
}
