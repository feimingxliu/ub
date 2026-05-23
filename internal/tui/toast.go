package tui

import (
	"fmt"
	"strings"

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

type toastState struct {
	text string
	tone toastTone
}

func (m *Model) showToast(tone toastTone, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.toast = toastState{text: text, tone: tone}
}

func (m *Model) clearToastForInteraction(msg tea.Msg) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg:
		m.toast = toastState{}
	}
}

func (m Model) toastView(width int) string {
	if strings.TrimSpace(m.toast.text) == "" {
		return ""
	}
	style := m.toastStyle()
	return m.styles.Render(style, truncateText("notice: "+m.toast.text, width))
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

func (m *Model) showToastForEvent(event Event) {
	switch event.Type {
	case EventActivity:
		m.showToastForActivity(event)
	case EventToolCallEnd:
		name := defaultString(event.ToolName, "tool")
		if event.IsError {
			m.showToast(toastFailure, fmt.Sprintf("tool %s failed", name))
			return
		}
		m.showToast(toastSuccess, fmt.Sprintf("tool %s succeeded", name))
	case EventPermission:
		if event.Allowed {
			m.showToast(toastSuccess, fmt.Sprintf("approval allowed %s", defaultString(event.ToolName, "tool")))
		}
	}
}

func (m *Model) showToastForActivity(event Event) {
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		name := defaultString(event.ToolName, "tool")
		switch strings.ToLower(strings.TrimSpace(event.Status)) {
		case "done":
			m.showToast(toastSuccess, fmt.Sprintf("tool %s succeeded", name))
		case "failed":
			m.showToast(toastFailure, fmt.Sprintf("tool %s failed", name))
		}
	case "permission":
		if event.Allowed {
			m.showToast(toastSuccess, fmt.Sprintf("approval allowed %s", defaultString(event.ToolName, "tool")))
		}
	}
}

func permissionDecisionAllows(decision permission.Decision) bool {
	switch decision {
	case permission.DecisionAllow, permission.DecisionAlwaysCmd, permission.DecisionAlwaysTool, permission.DecisionAlwaysGlobal:
		return true
	default:
		return false
	}
}
