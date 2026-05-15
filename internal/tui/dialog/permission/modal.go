// Package permissiondialog renders the TUI permission approval modal.
package permissiondialog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tui/diffview"
)

// Model is a small, focused permission modal state.
type Model struct {
	Request  permission.Request
	Expanded bool
	Diff     diffview.Model
}

// New creates a permission modal model.
func New(req permission.Request) Model {
	var diff diffview.Model
	if req.Preview != nil {
		diff = diffview.New(req.Preview.Files)
	}
	return Model{Request: req, Diff: diff}
}

// ToggleDiff toggles full preview diff rendering.
func (m Model) ToggleDiff() Model {
	m.Expanded = !m.Expanded
	return m
}

// HandleKey applies modal-local navigation keys.
func (m *Model) HandleKey(key string) bool {
	if !m.Expanded {
		return false
	}
	return m.Diff.HandleKey(key)
}

// SelectedDiffPath returns the selected diff path.
func (m Model) SelectedDiffPath() string {
	return m.Diff.SelectedPath()
}

// View renders the modal as plain text.
func (m Model) View() string {
	req := m.Request
	var b strings.Builder
	b.WriteString("Permission required\n")
	b.WriteString(fmt.Sprintf("tool: %s\n", fallback(req.Tool, "unknown")))
	b.WriteString(fmt.Sprintf("risk: %s\n", fallback(string(req.Risk), "unknown")))
	b.WriteString(fmt.Sprintf("mode: %s\n", fallback(string(req.Mode), "default")))
	if req.Mode == execution.ModePlan && req.Risk == tool.RiskExec {
		b.WriteString("Plan mode: command may still have side effects\n")
	}
	if strings.TrimSpace(req.ApprovalReason) != "" {
		b.WriteString("approval agent: ")
		b.WriteString(req.ApprovalReason)
		b.WriteByte('\n')
	}
	if len(req.Args) > 0 {
		b.WriteString("args: ")
		b.WriteString(compactJSON(req.Args))
		b.WriteByte('\n')
	}
	if req.Preview != nil {
		if strings.TrimSpace(req.Preview.Summary) != "" {
			b.WriteString("preview: ")
			b.WriteString(req.Preview.Summary)
			b.WriteByte('\n')
		}
		if len(req.Preview.Files) > 0 {
			if m.Expanded {
				b.WriteString(m.Diff.View())
				b.WriteByte('\n')
			} else {
				b.WriteString("press d to show diff\n")
			}
		}
	}
	b.WriteString("[1] Allow once  [2] Deny  [3] Always cmd  [4] Always tool  [5] Always tool global")
	return b.String()
}

// DecisionForKey maps modal keys to permission decisions.
func DecisionForKey(key string) (permission.Decision, bool) {
	switch key {
	case "1":
		return permission.DecisionAllow, true
	case "2":
		return permission.DecisionDeny, true
	case "3":
		return permission.DecisionAlwaysCmd, true
	case "4":
		return permission.DecisionAlwaysTool, true
	case "5":
		return permission.DecisionAlwaysGlobal, true
	default:
		return "", false
	}
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncate(string(raw), 240)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), 240)
	}
	return truncate(string(out), 240)
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
