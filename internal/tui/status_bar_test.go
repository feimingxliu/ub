package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

func TestStatusBarFitsLongModelAndCWD(t *testing.T) {
	bar := statusBar{
		provider:      "custom-openai-compatible-provider",
		model:         "local-glm5-anthropic/glm-5.1-reasoning-preview",
		effort:        "high",
		executionMode: "work",
		state:         statusIdle,
		cwd:           "/home/lfm/projects/feimingxliu/ub",
	}

	view := bar.view(80, tuitheme.Default())
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("status bar width = %d, want <= 80\n%s", got, view)
	}
	if strings.Contains(view, "provider:") || strings.Contains(view, "custom-openai-compatible-provider") {
		t.Fatalf("status bar should not render provider information:\n%s", view)
	}
	if !strings.Contains(view, "model:") {
		t.Fatalf("status bar missing model segment:\n%s", view)
	}
	if !strings.Contains(view, "?") {
		t.Fatalf("status bar missing help marker:\n%s", view)
	}
}

func TestStatusBarFitsDenseContext(t *testing.T) {
	bar := statusBar{
		provider:          "anthropic",
		model:             "local-glm5-anthropic/glm-5.1-reasoning-preview",
		effort:            "high",
		executionMode:     "plan",
		state:             statusStreaming,
		turn:              42,
		cwd:               "/home/lfm/projects/feimingxliu/ub",
		contextUsedTokens: 127000,
		contextMaxTokens:  200000,
		contextRatio:      0.635,
	}

	view := bar.view(80, tuitheme.Default())
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("dense status bar width = %d, want <= 80\n%s", got, view)
	}
	if !strings.Contains(view, "state:") {
		t.Fatalf("dense status bar should preserve state:\n%s", view)
	}
}
