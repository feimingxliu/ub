// Package tuitheme contains the small built-in visual theme used by the TUI.
package tuitheme

import "charm.land/lipgloss/v2"

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
	Tabs    lipgloss.Style
	Path    lipgloss.Style
	Kind    lipgloss.Style
	Help    lipgloss.Style
	Header  lipgloss.Style
	Added   lipgloss.Style
	Removed lipgloss.Style
	Context lipgloss.Style
}

// Default returns the built-in theme. The palette is restrained and keeps
// activity colors semantic rather than decorative.
func Default() Styles {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	blue := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	teal := lipgloss.NewStyle().Foreground(lipgloss.Color("43"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	orange := lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	rose := lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
	violet := lipgloss.NewStyle().Foreground(lipgloss.Color("141"))

	s := Styles{
		Muted:      muted,
		System:     muted,
		Error:      red.Bold(true),
		Focus:      bright.Copy().Background(lipgloss.Color("24")),
		SubtleLine: lipgloss.NewStyle().Foreground(lipgloss.Color("238")),
		Markdown: MarkdownStyles{
			StyleName: "notty",
		},
	}
	s.Role.UserPrefix = teal.Copy().Bold(true)
	s.Role.AssistantPrefix = blue.Copy().Bold(true)
	s.Role.UserText = text
	s.Role.AssistantText = text
	s.Role.SystemPrefix = amber.Copy().Bold(true)
	s.Role.ErrorPrefix = rose.Copy().Bold(true)

	s.Status.Bar = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	s.Status.Segment = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("235")).Padding(0, 1)
	s.Status.Label = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	s.Status.Value = bright
	s.Status.ModeWork = teal.Copy().Bold(true)
	s.Status.ModePlan = violet.Copy().Bold(true)
	s.Status.ModeAuto = amber.Copy().Bold(true)
	s.Status.StateIdle = green.Copy().Bold(true)
	s.Status.StateBusy = orange.Copy().Bold(true)
	s.Status.StateError = red.Copy().Bold(true)

	s.Picker.Title = blue.Copy().Bold(true)
	s.Picker.Item = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	s.Picker.Selected = bright.Copy().Background(lipgloss.Color("24")).Bold(true)
	s.Picker.Empty = muted

	s.Modal.Box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("60")).Padding(1, 2)
	s.Modal.Title = blue.Copy().Bold(true)
	s.Modal.Label = muted
	s.Modal.Value = text
	s.Modal.Warning = amber
	s.Modal.Selected = bright.Copy().Background(lipgloss.Color("24"))
	s.Modal.Option = text
	s.Modal.Help = muted
	s.Modal.PreviewLabel = teal

	s.Tool.Collapsed = teal.Copy().Background(lipgloss.Color("235"))
	s.Tool.Expanded = blue.Copy().Background(lipgloss.Color("235"))
	s.Tool.Running = amber
	s.Tool.Done = green
	s.Tool.Failed = red
	s.Tool.Denied = rose
	s.Tool.Detail = muted
	s.Tool.Focus = s.Focus

	s.Thinking.Collapsed = violet.Copy().Background(lipgloss.Color("235"))
	s.Thinking.Expanded = violet.Copy().Background(lipgloss.Color("235"))
	s.Thinking.Running = violet
	s.Thinking.Done = muted
	s.Thinking.Failed = red
	s.Thinking.Detail = muted
	s.Thinking.Focus = s.Focus

	s.Input.Prompt = teal.Copy().Bold(true)
	s.Input.Text = text
	s.Input.Placeholder = muted.Copy().Italic(true)
	s.Input.Cursor = teal.Copy().Reverse(true)

	s.Diff.Tabs = muted
	s.Diff.Path = teal.Copy().Bold(true)
	s.Diff.Kind = amber
	s.Diff.Help = muted
	s.Diff.Header = violet.Copy().Bold(true)
	s.Diff.Added = green
	s.Diff.Removed = red
	s.Diff.Context = text
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
