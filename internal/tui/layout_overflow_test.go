package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

// On resume, persisted activity summaries can contain embedded newlines (e.g.
// the model's thinking summary kept its "\n\n" paragraph breaks). When such an
// activity is rendered collapsed as a chip, the chip text inherits the
// newline. messageList.view then joins its line slice with "\n" and frame.go
// splits the joined string back, producing MORE rows than messageViewHeight
// allotted — pushing the footer/input below the terminal edge. Pin the
// invariant that every rendered line is a single terminal row (no embedded
// "\n").
func TestCollapsedThinkingChipDoesNotEmbedNewlines(t *testing.T) {
	const width = 80
	styles := tuitheme.Default()

	multiline := "Now I've read through the entire codebase.\n\n1. Thumbnails bug - already fixed\n\n2. Unit testing - missing"
	entry := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Summary:      multiline,
		Content:      multiline,
	})
	entry.collapsed = true

	list := messageList{focus: -1, entryFocus: -1, items: []message{entry}}
	rendered := list.render(width, styles)
	for i, line := range rendered.lines {
		if strings.Contains(line, "\n") {
			t.Fatalf("rendered line %d contains embedded newline: %q", i, line)
		}
	}
}

// Probe whether rendering an expanded tool diff produces lines whose visual
// width exceeds the configured content width. If any line overflows, the
// terminal will auto-wrap it on top of the footer / input area, which
// matches the "thinking block overlays input" symptom reported after
// running write/edit on README-style files.
func TestToolDiffDetailFitsContentWidth(t *testing.T) {
	const width = 80
	styles := tuitheme.Default()

	diff := strings.Join([]string{
		"--- README.md",
		"+++ README.md",
		"@@ -1,4 +1,4 @@",
		" # ub",
		" ",
		"-ub 是一个用 Go 编写的终端编码 Agent，包含 CLI 和 TUI 两种入口。它的核心链路是本地优先的：Provider 适配层负责模型流式输出。",
		"+ub 是一个用 Go 编写的终端编码 Agent，包含 CLI 和 TUI 两种入口。它的核心链路是本地优先的：Provider 适配层负责模型流式输出，工具在当前工作区执行，权限策略拦截有副作用的操作。",
	}, "\n")

	entry := message{
		role:      activityRole,
		kind:      toolMessage,
		title:     "write README.md",
		name:      "fs.write",
		status:    "done",
		detail:    diff,
		collapsed: false,
	}
	group := message{
		role:      activityRole,
		kind:      activityGroupMessage,
		name:      toolGroupName,
		title:     "Tools",
		status:    "done",
		entries:   []message{entry},
		collapsed: false,
	}

	list := messageList{focus: -1, entryFocus: -1, items: []message{group}}
	rendered := list.render(width, styles)
	limit := contentWidth(width)
	for i, line := range rendered.lines {
		got := lipgloss.Width(xansi.Strip(line))
		if got > limit {
			t.Fatalf("line %d visual width = %d, exceeds content width %d:\n%q", i, got, limit, xansi.Strip(line))
		}
	}
}

// `fs.read` returns "<line-number>\t<text>" per line. The TAB character is
// reported as width 0 by runewidth, so wrapLine drastically under-counts the
// visual width of a read tool result row. When this detail expands inside the
// TUI, individual lines exceed the column width and the terminal auto-wraps,
// breaking layout alignment (footer / input gets pushed out, clicks land on
// the wrong spans). This test pins that behavior so a fix can flip it.
func TestReadToolDetailFitsContentWidthWithTabs(t *testing.T) {
	const width = 80
	styles := tuitheme.Default()

	// Mimic formatNumberedLines: "%*d\t%s" — every row carries a TAB.
	// runewidth.RuneWidth('\t') == 0, so wrapLine sees the row width as
	// 2 (line number) + 0 (tab) + 75 (body) = 77, which is <= textWidth(78)
	// and therefore returns the row UNCHANGED (no wrap, tab preserved).
	// When the terminal then renders the row, the tab expands to the next
	// 8-column stop, producing:
	//   prefix(2) + "NN"(2) + tab-jump-to-col-8(4) + 75 body cols = 83 cols
	// which overflows the 80-col terminal by 3 columns and triggers
	// auto-wrap on every numbered line — the exact symptom that pushed
	// the footer/input up under the thinking block.
	body := strings.Repeat("x", 75)
	var lines []string
	for i := 1; i <= 4; i++ {
		lines = append(lines, sprintLineNumbered(i, body))
	}
	detail := strings.Join(lines, "\n")

	entry := message{
		role:      activityRole,
		kind:      toolMessage,
		title:     "read README.md",
		name:      "read",
		status:    "done",
		detail:    detail,
		collapsed: false,
	}
	group := message{
		role:      activityRole,
		kind:      activityGroupMessage,
		name:      toolGroupName,
		title:     "Tools",
		status:    "done",
		entries:   []message{entry},
		collapsed: false,
	}
	list := messageList{focus: -1, entryFocus: -1, items: []message{group}}

	rendered := list.render(width, styles)
	limit := contentWidth(width)
	for i, line := range rendered.lines {
		stripped := xansi.Strip(line)
		// Visual width as the terminal will draw it: tabs expand to next
		// 8-col stop (the POSIX default; many terminals use this).
		visual := visualWidthExpandingTabs(stripped, 8)
		if visual > limit {
			t.Fatalf("line %d visual width (tab-expanded) = %d, exceeds content width %d:\n%q",
				i, visual, limit, stripped)
		}
	}
}

func sprintLineNumbered(n int, body string) string {
	return fmt.Sprintf("%2d\t%s", n, body)
}

func visualWidthExpandingTabs(s string, tabStop int) int {
	col := 0
	for _, r := range s {
		if r == '\t' {
			col += tabStop - (col % tabStop)
			continue
		}
		col += lipgloss.Width(string(r))
	}
	return col
}

// Simulate the reported scenario: a tool group with an expanded README diff is
// already in the messageList, then a new thinking activity starts (next turn).
// The View() output must fit within m.height rows in BOTH logical newline
// count AND per-line visual width — otherwise the terminal will wrap individual
// lines and push the input area off-screen / overlap with the thinking block.
func TestNextTurnAfterDiffDoesNotOverflowFrame(t *testing.T) {
	const (
		width  = 80
		height = 24
	)

	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = assertModel(t, updated)

	// Simulate the prior turn: user message + tool group with an expanded
	// README diff entry.
	model.messages.append(userRole, "请把 README 改一下")
	diff := strings.Join([]string{
		"--- README.md",
		"+++ README.md",
		"@@ -1,4 +1,4 @@",
		" # ub",
		" ",
		"-ub 是一个用 Go 编写的终端编码 Agent。",
		"+ub 是一个用 Go 编写的终端编码 Agent，包含 CLI 和 TUI 两种入口。",
	}, "\n")
	entry := message{
		role:      activityRole,
		kind:      toolMessage,
		title:     "write README.md",
		name:      "fs.write",
		status:    "done",
		detail:    diff,
		collapsed: false,
	}
	model.messages.items = append(model.messages.items, message{
		role:      activityRole,
		kind:      activityGroupMessage,
		name:      toolGroupName,
		title:     "Tools",
		status:    "done",
		entries:   []message{entry},
		collapsed: false,
	})
	model.messages.append(assistantRole, "已修改完成。")

	// New turn: thinking activity arrives.
	model.messages.startActivityGroup("t-thinking-2", "Thinking...")
	model.running = true
	model.status.state = statusThinking

	view := model.View().Content
	lines := strings.Split(view, "\n")

	if len(lines) > height {
		t.Fatalf("rendered %d logical lines, exceeds terminal height %d:\n%s", len(lines), height, view)
	}
	limit := contentWidth(width)
	for i, line := range lines {
		got := lipgloss.Width(xansi.Strip(line))
		if got > limit {
			t.Fatalf("line %d visual width = %d, exceeds content width %d:\n%q\n---FULL---\n%s",
				i, got, limit, xansi.Strip(line), view)
		}
	}
}
