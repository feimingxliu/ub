// Package tuitheme contains the small built-in visual theme used by the TUI.
package tuitheme

import "github.com/charmbracelet/lipgloss"

// Styles groups semantic styles for the terminal UI. It is intentionally small:
// callers should ask for role/status/tool meaning instead of hard-coding colors.
type Styles struct {
	Plain bool

	Role       RoleStyles
	Status     StatusStyles
	Picker     PickerStyles
	Modal      ModalStyles
	Tool       ActivityStyles
	Thinking   ActivityStyles
	Markdown   MarkdownStyles
	Input      InputStyles
	Diff       DiffStyles
	System     lipgloss.Style
	Error      lipgloss.Style
	Muted      lipgloss.Style
	Focus      lipgloss.Style
	SubtleLine lipgloss.Style
}

type RoleStyles struct {
	UserPrefix      lipgloss.Style
	AssistantPrefix lipgloss.Style
	UserText        lipgloss.Style
	AssistantText   lipgloss.Style
	SystemPrefix    lipgloss.Style
	ErrorPrefix     lipgloss.Style
}

type StatusStyles struct {
	Bar        lipgloss.Style
	Segment    lipgloss.Style
	Label      lipgloss.Style
	Value      lipgloss.Style
	ModeWork   lipgloss.Style
	ModePlan   lipgloss.Style
	ModeAuto   lipgloss.Style
	StateIdle  lipgloss.Style
	StateBusy  lipgloss.Style
	StateError lipgloss.Style
}

type PickerStyles struct {
	Title    lipgloss.Style
	Item     lipgloss.Style
	Selected lipgloss.Style
	Empty    lipgloss.Style
}

type ModalStyles struct {
	Box          lipgloss.Style
	Title        lipgloss.Style
	Label        lipgloss.Style
	Value        lipgloss.Style
	Warning      lipgloss.Style
	Selected     lipgloss.Style
	Option       lipgloss.Style
	Help         lipgloss.Style
	PreviewLabel lipgloss.Style
}

type ActivityStyles struct {
	Collapsed lipgloss.Style
	Expanded  lipgloss.Style
	Running   lipgloss.Style
	Done      lipgloss.Style
	Failed    lipgloss.Style
	Denied    lipgloss.Style
	Detail    lipgloss.Style
	Focus     lipgloss.Style
}

type MarkdownStyles struct {
	StyleName string
}

type InputStyles struct {
	Prompt      lipgloss.Style
	Text        lipgloss.Style
	Placeholder lipgloss.Style
	Cursor      lipgloss.Style
}

type DiffStyles struct {
	Tabs lipgloss.Style
	Path lipgloss.Style
	Kind lipgloss.Style
	Help lipgloss.Style
}

// Default returns the built-in theme. The palette is restrained and keeps
// activity colors semantic rather than decorative.
func Default() Styles {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	violet := lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("45"))

	s := Styles{
		Muted:      muted,
		System:     muted,
		Error:      red.Bold(true),
		Focus:      lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")),
		SubtleLine: muted,
		Markdown: MarkdownStyles{
			StyleName: "notty",
		},
	}
	s.Role.UserPrefix = cyan.Bold(true)
	s.Role.AssistantPrefix = muted
	s.Role.UserText = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Role.AssistantText = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Role.SystemPrefix = muted.Bold(true)
	s.Role.ErrorPrefix = red.Bold(true)

	s.Status.Bar = lipgloss.NewStyle()
	s.Status.Segment = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	s.Status.Label = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	s.Status.Value = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	s.Status.ModeWork = accent.Copy().Bold(true)
	s.Status.ModePlan = violet.Copy().Bold(true)
	s.Status.ModeAuto = yellow.Copy().Bold(true)
	s.Status.StateIdle = green.Copy().Bold(true)
	s.Status.StateBusy = yellow.Copy().Bold(true)
	s.Status.StateError = red.Copy().Bold(true)

	s.Picker.Title = muted.Bold(true)
	s.Picker.Item = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Picker.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238"))
	s.Picker.Empty = muted

	s.Modal.Box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(1, 2)
	s.Modal.Title = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	s.Modal.Label = muted
	s.Modal.Value = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Modal.Warning = yellow
	s.Modal.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238"))
	s.Modal.Option = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Modal.Help = muted
	s.Modal.PreviewLabel = accent

	s.Tool.Collapsed = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Tool.Expanded = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Tool.Running = yellow
	s.Tool.Done = green
	s.Tool.Failed = red
	s.Tool.Denied = red
	s.Tool.Detail = muted
	s.Tool.Focus = s.Focus

	s.Thinking.Collapsed = violet
	s.Thinking.Expanded = violet
	s.Thinking.Running = violet
	s.Thinking.Done = muted
	s.Thinking.Failed = red
	s.Thinking.Detail = muted
	s.Thinking.Focus = s.Focus

	s.Input.Prompt = accent.Bold(true)
	s.Input.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	s.Input.Placeholder = muted
	s.Input.Cursor = accent.Reverse(true)

	s.Diff.Tabs = muted
	s.Diff.Path = accent.Bold(true)
	s.Diff.Kind = muted
	s.Diff.Help = muted
	return s
}

// Plain returns styles that preserve the same text and symbols without ANSI.
func Plain() Styles {
	return Styles{Plain: true, Markdown: MarkdownStyles{StyleName: "notty"}}
}

// Render applies a style unless the theme is in plain mode.
func (s Styles) Render(style lipgloss.Style, value string) string {
	if s.Plain {
		return value
	}
	return style.Render(value)
}
