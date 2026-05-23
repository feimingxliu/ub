package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/feimingxliu/ub/internal/permission"
)

type toastTone string

const (
	toastSuccess toastTone = "success"
	toastFailure toastTone = "failure"
	toastNotice  toastTone = "notice"
)

// toastTTL bounds how long a toast stays on screen without further input.
const toastTTL = 3 * time.Second

type toastState struct {
	text       string
	tone       toastTone
	generation int
}

type toastExpireMsg struct {
	generation int
}

func (m *Model) showToast(tone toastTone, text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	gen := m.toast.generation + 1
	m.toast = toastState{text: text, tone: tone, generation: gen}
	return tea.Tick(toastTTL, func(time.Time) tea.Msg {
		return toastExpireMsg{generation: gen}
	})
}

func (m *Model) handleToastExpire(msg toastExpireMsg) {
	if msg.generation == m.toast.generation {
		m.toast = toastState{generation: m.toast.generation}
	}
}

func (m *Model) clearToastForInteraction(msg tea.Msg) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg:
		m.toast = toastState{generation: m.toast.generation}
	}
}

func (m Model) toastView(width int) string {
	if strings.TrimSpace(m.toast.text) == "" {
		return ""
	}
	style := m.toastStyle()
	return m.styles.Render(style, truncateText(toastPrefix(m.toast.tone)+m.toast.text, width))
}

func toastPrefix(tone toastTone) string {
	switch tone {
	case toastSuccess:
		return "ok: "
	case toastFailure:
		return "error: "
	default:
		return "notice: "
	}
}

func (m Model) toastStyle() lipgloss.Style {
	switch m.toast.tone {
	case toastSuccess:
		return m.styles.Tool.Done
	case toastFailure:
		return m.styles.Tool.Failed
	default:
		return m.styles.Tool.Running
	}
}

// showToastForEvent surfaces a transient toast for tool completions and
// permission grants. Tool completions are emitted as both EventActivity
// and EventToolCallEnd; we only react to EventActivity here so the same
// completion does not double-toast.
func (m *Model) showToastForEvent(event Event) tea.Cmd {
	switch event.Type {
	case EventActivity:
		return m.showToastForActivity(event)
	case EventPermission:
		if event.Allowed {
			return m.showToast(toastSuccess, fmt.Sprintf("approval allowed %s", defaultString(event.ToolName, "tool")))
		}
	}
	return nil
}

func (m *Model) showToastForActivity(event Event) tea.Cmd {
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		name := defaultString(event.ToolName, "tool")
		switch strings.ToLower(strings.TrimSpace(event.Status)) {
		case "done":
			return m.showToast(toastSuccess, fmt.Sprintf("tool %s succeeded", name))
		case "failed":
			return m.showToast(toastFailure, fmt.Sprintf("tool %s failed", name))
		}
	case "permission":
		if event.Allowed {
			return m.showToast(toastSuccess, fmt.Sprintf("approval allowed %s", defaultString(event.ToolName, "tool")))
		}
	}
	return nil
}

func permissionDecisionAllows(decision permission.Decision) bool {
	switch decision {
	case permission.DecisionAllow, permission.DecisionAlwaysCmd, permission.DecisionAlwaysTool, permission.DecisionAlwaysGlobal:
		return true
	default:
		return false
	}
}
