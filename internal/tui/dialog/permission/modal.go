// Package permissiondialog renders the TUI permission approval modal.
package permissiondialog

import (
	"encoding/json"
	"strings"

	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tui/diffview"
	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

// Model is a small, focused permission modal state.
type Model struct {
	Request  permission.Request
	Expanded bool
	Diff     diffview.Model
	selected int
}

type option struct {
	Decision    permission.Decision
	Label       string
	Description string
}

var options = []option{
	{
		Decision:    permission.DecisionAllow,
		Label:       "Allow once",
		Description: "Run only this request. No rule is saved; similar requests will ask again.",
	},
	{
		Decision:    permission.DecisionDeny,
		Label:       "Deny",
		Description: "Block this request and leave permission rules unchanged.",
	},
	{
		Decision:    permission.DecisionAlwaysCmd,
		Label:       "Always allow exact command in this session",
		Description: "Allow this tool only when the command text matches exactly until this session exits.",
	},
	{
		Decision:    permission.DecisionAlwaysTool,
		Label:       "Always allow this tool in this session",
		Description: "Allow future calls to this tool until this session exits. Wider than exact command.",
	},
	{
		Decision:    permission.DecisionAlwaysProjectCmd,
		Label:       "Always allow exact command in this project",
		Description: "Persist a project-local allow rule for this exact command in .ub/permissions.yaml.",
	},
	{
		Decision:    permission.DecisionAlwaysProjectPattern,
		Label:       "Always allow similar command in this project",
		Description: "Persist Claude-style wildcard rules such as 'go test:*' in .ub/permissions.yaml.",
	},
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
	switch key {
	case "up", "k":
		m.previousOption()
		return true
	case "down", "j", "tab":
		m.nextOption()
		return true
	case "left", "right":
		if m.Expanded {
			return m.Diff.HandleKey(key)
		}
		return false
	default:
		return false
	}
}

// SelectedDiffPath returns the selected diff path.
func (m Model) SelectedDiffPath() string {
	return m.Diff.SelectedPath()
}

// View renders the modal.
func (m Model) View() string {
	styles := tuitheme.Default()
	req := m.Request
	var b strings.Builder
	b.WriteString(styles.Render(styles.Modal.Title, "Permission required"))
	b.WriteByte('\n')
	b.WriteString(fieldLine(styles, "tool", fallback(req.Tool, "unknown")))
	b.WriteString(fieldLine(styles, "risk", fallback(string(req.Risk), "unknown")))
	b.WriteString(fieldLine(styles, "mode", fallback(string(req.Mode), "default")))
	if strings.TrimSpace(req.ApprovalReason) != "" {
		b.WriteString(fieldLine(styles, "approval agent", req.ApprovalReason))
	}
	if len(req.Args) > 0 {
		b.WriteString(fieldLine(styles, "args", compactJSON(req.Args)))
	}
	if req.Preview != nil {
		if strings.TrimSpace(req.Preview.Summary) != "" {
			b.WriteString(fieldLine(styles, "preview", req.Preview.Summary))
		}
		if len(req.Preview.Files) > 0 {
			if m.Expanded {
				b.WriteString(m.Diff.View())
				b.WriteByte('\n')
			} else {
				b.WriteString(styles.Render(styles.Modal.Help, "press d to show diff"))
				b.WriteByte('\n')
			}
		}
	}
	b.WriteString(styles.Render(styles.Modal.Help, "choose an action (up/down, enter to confirm)"))
	b.WriteByte('\n')
	for i, option := range options {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		line := marker + option.Label
		if strings.TrimSpace(option.Description) != "" {
			line += " - " + option.Description
		}
		if i == m.selected {
			b.WriteString(styles.Render(styles.Modal.Selected, line))
		} else {
			b.WriteString(styles.Render(styles.Modal.Option, line))
		}
		b.WriteByte('\n')
	}
	b.WriteString(styles.Render(styles.Modal.Help, "shortcuts: 1-6"))
	return styles.Render(styles.Modal.Box, b.String())
}

func fieldLine(styles tuitheme.Styles, label, value string) string {
	return styles.Render(styles.Modal.Value, label+": "+value) + "\n"
}

// SelectedDecision returns the currently highlighted decision.
func (m Model) SelectedDecision() permission.Decision {
	if len(options) == 0 {
		return ""
	}
	if m.selected < 0 {
		return options[0].Decision
	}
	if m.selected >= len(options) {
		return options[len(options)-1].Decision
	}
	return options[m.selected].Decision
}

func (m *Model) nextOption() {
	if len(options) == 0 {
		return
	}
	m.selected = (m.selected + 1) % len(options)
}

func (m *Model) previousOption() {
	if len(options) == 0 {
		return
	}
	m.selected = (m.selected + len(options) - 1) % len(options)
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
		return permission.DecisionAlwaysProjectCmd, true
	case "6":
		return permission.DecisionAlwaysProjectPattern, true
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
