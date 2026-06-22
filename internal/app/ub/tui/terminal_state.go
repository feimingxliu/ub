package tui

import (
	"io"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
)

// inputMaxHeight caps the auto-growing textarea at roughly a third of the
// terminal height so multiline input never crowds out the message area.
func inputMaxHeight(height int) int {
	return max(1, frameHeight(height)/3)
}

// inputContentWidth returns the column width available for the textarea's own
// content. The "› " prompt is prepended externally (frame.go), so the textarea
// must be narrower by that prefix's width to keep each rendered line within
// contentWidth.
func inputContentWidth(width int) int {
	return max(1, contentWidth(width)-runewidth.StringWidth(inputPromptPrefix))
}

func detectInitialWindowSize(output io.Writer) (int, int) {
	// Precedence follows the standard Unix convention: COLUMNS/LINES override
	// auto-detection. This lets users opt into a fixed window (e.g. for tests
	// or recordings) without the auto-detect path silently clamping it back
	// to the real terminal size.
	if envWidth, envHeight, ok := envWindowSize(); ok {
		return normalizedWindowSize(envWidth, envHeight)
	}
	if width, height, ok := terminalWindowSize(output); ok {
		return normalizedWindowSize(width, height)
	}
	return normalizedWindowSize(defaultViewWidth, defaultViewHeight)
}

func terminalWindowSize(output io.Writer) (int, int, bool) {
	if output == nil {
		output = os.Stdout
	}
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, false
	}
	width, height, err := term.GetSize(file.Fd())
	if err != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func envWindowSize() (int, int, bool) {
	width, errWidth := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	height, errHeight := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func normalizedWindowSize(width, height int) (int, int) {
	return contentWidth(width), frameHeight(height)
}

func windowSizeCmd(width, height int) tea.Cmd {
	width, height = normalizedWindowSize(width, height)
	return func() tea.Msg {
		return tea.WindowSizeMsg{Width: width, Height: height}
	}
}

func requestWindowSize() tea.Cmd {
	return func() tea.Msg {
		return tea.RequestWindowSize()
	}
}

// textareaTextStyles sets the palette for the multiline textarea. The prompt
// is empty because the "› " prefix is prepended externally in frame.go (only
// on the first visual line), so the textarea's own per-line prompt must stay
// blank to avoid duplicate markers.
func textareaTextStyles() textarea.Styles {
	styles := textarea.DefaultDarkStyles()
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	styles.Focused.Text = text
	styles.Focused.Placeholder = placeholder
	styles.Focused.Prompt = lipgloss.NewStyle()
	styles.Focused.EndOfBuffer = lipgloss.NewStyle()
	styles.Blurred.Text = text
	styles.Blurred.Placeholder = placeholder
	styles.Blurred.Prompt = lipgloss.NewStyle()
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle()
	styles.Cursor.Color = lipgloss.Color("43")
	styles.Cursor.Shape = tea.CursorBlock
	styles.Cursor.Blink = false
	return styles
}

// inputKeyMap customizes the textarea keybindings so they coexist with ub's
// own input handling:
//   - InsertNewline is rebound to shift+enter / ctrl+j (plain Enter submits).
//   - LineNext/LinePrevious are unbound from down/up (ub intercepts those for
//     smart history navigation / completion selection) and moved to alt+down/up.
//
// All other bindings keep textarea defaults (backspace, left/right, ctrl+u/k/w,
// home/end, etc.).
func inputKeyMap() textarea.KeyMap {
	km := textarea.DefaultKeyMap()
	km.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "insert newline"))
	km.LineNext = key.NewBinding(key.WithKeys("alt+down"), key.WithHelp("alt+down", "next line"))
	km.LinePrevious = key.NewBinding(key.WithKeys("alt+up"), key.WithHelp("alt+up", "previous line"))
	return km
}
