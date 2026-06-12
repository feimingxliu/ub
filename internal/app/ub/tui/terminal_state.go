package tui

import (
	"io"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
)

func inputWidthForTerminal(width int, prompt string) int {
	available := contentWidth(width) - runewidth.StringWidth(prompt) - 1
	return max(1, available)
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

func inputTextStyles() textinput.Styles {
	styles := textinput.DefaultDarkStyles()
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("43")).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	styles.Focused.Prompt = prompt
	styles.Focused.Text = text
	styles.Focused.Placeholder = placeholder
	styles.Blurred.Prompt = prompt
	styles.Blurred.Text = text
	styles.Blurred.Placeholder = placeholder
	styles.Cursor.Color = lipgloss.Color("43")
	styles.Cursor.Shape = tea.CursorBlock
	styles.Cursor.Blink = false
	return styles
}
