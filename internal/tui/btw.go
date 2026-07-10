package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

const sideQuestionWheelScrollLines = 3

type sideQuestionEntry struct {
	question string
	answer   string
	err      string
}

type sideQuestionState struct {
	visible bool
	running bool
	runID   int
	entries []sideQuestionEntry
	draft   string
	scroll  int
	events  <-chan Event
	cancel  context.CancelFunc

	renderVersion int
	bodyCache     map[string][]string
}

func newSideQuestionState() sideQuestionState {
	return sideQuestionState{bodyCache: map[string][]string{}}
}

func (s *sideQuestionState) invalidateRender() {
	s.renderVersion++
	if s.bodyCache == nil {
		s.bodyCache = map[string][]string{}
		return
	}
	for key := range s.bodyCache {
		delete(s.bodyCache, key)
	}
}

func isSideQuestionInput(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(strings.ToLower(text), "/btw") {
		return false
	}
	if len(text) == len("/btw") {
		return true
	}
	return strings.ContainsAny(text[len("/btw"):len("/btw")+1], " \t\r\n")
}

func (m Model) startSideQuestion(args []string) (tea.Model, tea.Cmd) {
	question := strings.TrimSpace(strings.Join(args, " "))
	if question == "" {
		m.btw.visible = true
		m.btw.scroll = 0
		return m, nil
	}
	return m.startSideQuestionText(question)
}

func (m Model) startSideQuestionText(question string) (tea.Model, tea.Cmd) {
	question = strings.TrimSpace(question)
	if question == "" {
		return m, nil
	}
	if m.btw.running {
		return m, m.showToast(toastNotice, "btw is already asking")
	}
	runner, ok := m.runner.(SideQuestionRunner)
	if !ok {
		return m, m.showToast(toastFailure, "btw is unavailable in this runner")
	}
	req := SideQuestionRequest{
		Question: question,
		History:  m.btw.history(),
	}
	ctx, cancel := context.WithCancel(m.ctx)
	events := make(chan Event, 32)
	runID := m.btw.runID + 1
	m.btw.visible = true
	m.btw.running = true
	m.btw.runID = runID
	m.btw.draft = ""
	m.btw.scroll = 0
	m.btw.events = events
	m.btw.cancel = cancel
	m.btw.entries = append(m.btw.entries, sideQuestionEntry{question: question})
	m.btw.invalidateRender()
	return m, tea.Batch(runSideQuestion(ctx, runner, req, events), waitForSideQuestionEvent(events, runID))
}

func (m Model) handleSideQuestionEvent(msg sideQuestionEventMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.btw.runID {
		return m, nil
	}
	if !msg.ok {
		m.finishSideQuestion()
		return m, nil
	}
	switch msg.event.Type {
	case EventDeltaText:
		m.appendSideQuestionAnswer(msg.event.Text)
		return m, waitForSideQuestionEvent(m.btw.events, m.btw.runID)
	case EventDone:
		wasHidden := !m.btw.visible
		m.finishSideQuestion()
		if wasHidden {
			return m, m.showToast(toastSuccess, "btw answer ready")
		}
		return m, nil
	case EventError:
		m.setSideQuestionError(defaultString(msg.event.Content, "btw failed"))
		m.finishSideQuestion()
		m.btw.visible = true
		return m, nil
	default:
		return m, waitForSideQuestionEvent(m.btw.events, m.btw.runID)
	}
}

func (m *Model) appendSideQuestionAnswer(text string) {
	if len(m.btw.entries) == 0 {
		m.btw.entries = append(m.btw.entries, sideQuestionEntry{})
	}
	i := len(m.btw.entries) - 1
	m.btw.entries[i].answer += text
	m.btw.scroll = 0
	m.btw.invalidateRender()
}

func (m *Model) setSideQuestionError(text string) {
	if len(m.btw.entries) == 0 {
		m.btw.entries = append(m.btw.entries, sideQuestionEntry{})
	}
	i := len(m.btw.entries) - 1
	m.btw.entries[i].err = text
	m.btw.scroll = 0
	m.btw.invalidateRender()
}

func (m *Model) finishSideQuestion() {
	m.btw.running = false
	m.btw.events = nil
	if m.btw.cancel != nil {
		m.btw.cancel()
	}
	m.btw.cancel = nil
	m.btw.invalidateRender()
}

func (m *Model) closeSideQuestionView() {
	if m.btw.cancel != nil {
		m.btw.cancel()
	}
	m.btw.visible = false
	m.btw.running = false
	m.btw.runID++
	m.btw.entries = nil
	m.btw.draft = ""
	m.btw.scroll = 0
	m.btw.events = nil
	m.btw.cancel = nil
	m.btw.invalidateRender()
}

func (m Model) handleSideQuestionKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch key.String() {
	case "ctrl+c":
		return m, nil, false
	case "esc":
		m.closeSideQuestionView()
		return m, nil, true
	case "enter":
		if m.btw.running {
			return m, m.showToast(toastNotice, "btw is already asking"), true
		}
		question := strings.TrimSpace(m.btw.draft)
		if question == "" {
			return m, nil, true
		}
		updated, cmd := m.startSideQuestionText(question)
		return updated, cmd, true
	case "pgup":
		m.scrollSideQuestion(m.pageScrollLines())
		return m, nil, true
	case "pgdown":
		m.scrollSideQuestion(-m.pageScrollLines())
		return m, nil, true
	case "ctrl+home":
		m.scrollSideQuestionToTop()
		return m, nil, true
	case "ctrl+end":
		m.scrollSideQuestionToBottom()
		return m, nil, true
	case "ctrl+y":
		return m.copySideQuestionAnswer()
	case "ctrl+u":
		if m.btw.running {
			return m, m.showToast(toastNotice, "btw is still running"), true
		}
		m.btw.entries = nil
		m.btw.draft = ""
		m.btw.scroll = 0
		m.btw.visible = true
		m.btw.invalidateRender()
		return m, nil, true
	case "backspace", "delete":
		if m.btw.running {
			return m, nil, true
		}
		m.btw.draft = dropLastRune(m.btw.draft)
		return m, nil, true
	default:
		if m.btw.running {
			return m, nil, true
		}
		if key.String() == "space" && key.Text == "" {
			m.btw.draft += " "
			return m, nil, true
		}
		if key.Text != "" {
			m.btw.draft += key.Text
		}
		return m, nil, true
	}
}

func dropLastRune(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	return string(runes[:len(runes)-1])
}

func (m Model) copySideQuestionAnswer() (tea.Model, tea.Cmd, bool) {
	text := strings.TrimSpace(m.btw.latestAnswer())
	if text == "" {
		return m, m.showToast(toastNotice, "btw answer is empty"), true
	}
	clipboard := m.clipboard
	ctx := m.ctx
	return m, func() tea.Msg {
		if err := clipboard.WriteText(ctx, text); err != nil {
			return copyResultMsg{label: "btw answer", err: err}
		}
		return copyResultMsg{label: "btw answer"}
	}, true
}

func (m Model) renderSideQuestionFrame() tuiFrame {
	width := contentWidth(m.width)
	height := frameHeight(m.height)
	if height <= 0 {
		return tuiFrame{}
	}
	body := m.sideQuestionBodyLines(width)
	footer := m.sideQuestionFooterLines(width, 0, 0)
	draftVisible := true
	if len(footer) > height {
		draftVisible = len(footer)-height == 0
		footer = footer[len(footer)-height:]
	}
	bodyHeight := max(0, height-len(footer))
	maxScroll := maxSideQuestionScrollFor(len(body), bodyHeight)
	clampedScroll := m.clampedSideQuestionScrollFor(maxScroll)
	footer = m.sideQuestionFooterLines(width, maxScroll, clampedScroll)
	if len(footer) > height {
		draftVisible = len(footer)-height == 0
		footer = footer[len(footer)-height:]
		bodyHeight = 0
		maxScroll = 0
		clampedScroll = 0
	}
	start := visibleStart(len(body), bodyHeight, clampedScroll)
	end := min(len(body), start+bodyHeight)

	lines := append([]string(nil), body[start:end]...)
	for len(lines) < bodyHeight {
		lines = append(lines, "")
	}
	lines = append(lines, footer...)
	lines = padFrameLines(lines, width, height)
	return tuiFrame{
		content: strings.Join(lines, "\n"),
		cursor:  m.sideQuestionCursor(width, bodyHeight, draftVisible),
	}
}

func (m Model) sideQuestionBodyLines(width int) []string {
	cacheKey := m.sideQuestionBodyCacheKey(width)
	if m.btw.bodyCache != nil {
		if lines, ok := m.btw.bodyCache[cacheKey]; ok {
			return lines
		}
	}
	title := "BTW"
	if count := len(m.btw.entries); count > 0 {
		title += fmt.Sprintf(" (%d)", count)
	}
	if m.btw.running {
		title += " asking..."
	}
	lines := []string{m.styles.Render(m.styles.Picker.Title, truncateText(title, width))}
	lines = append(lines, "")
	lines = append(lines, m.sideQuestionEntryLines(width)...)
	if m.btw.bodyCache != nil {
		m.btw.bodyCache[cacheKey] = lines
	}
	return lines
}

func (m Model) sideQuestionBodyCacheKey(width int) string {
	styleName := m.styles.Markdown.StyleName
	if m.styles.Plain {
		styleName = "plain"
	}
	return fmt.Sprintf("%d:%t:%s:%d", width, m.styles.Plain, styleName, m.btw.renderVersion)
}

func (m Model) sideQuestionFooterLines(width, maxScroll, clampedScroll int) []string {
	lines := []string{m.sideQuestionDraftLine(width)}
	help := "Enter ask · PgUp/PgDown scroll · Esc return & clear"
	if maxScroll > 0 {
		help += fmt.Sprintf(" · %d/%d", clampedScroll, maxScroll)
	}
	if strings.TrimSpace(m.btw.latestAnswer()) != "" {
		help += " · Ctrl+Y copy latest"
	}
	if !m.btw.running {
		help += " · Ctrl+U clear"
	}
	lines = append(lines, m.styles.Render(m.styles.Modal.Help, truncateText(help, width)))
	if toast := m.toastView(width); toast != "" {
		lines = append(lines, splitFrameLines(toast)...)
	}
	lines = append(lines, m.sideQuestionStatusLine(width))
	return lines
}

func (m Model) sideQuestionEntryLines(width int) []string {
	if len(m.btw.entries) == 0 {
		return []string{m.styles.Render(m.styles.Muted, truncateText("  no BTW questions yet", width))}
	}
	var lines []string
	for i, entry := range m.btw.entries {
		if i > 0 {
			lines = append(lines, "")
		}
		number := i + 1
		qPrefix := fmt.Sprintf("  Q%d: ", number)
		lines = append(lines, renderWrappedPrefixed(entry.question, qPrefix, strings.Repeat(" ", len(qPrefix)), width, m.styles, m.styles.Role.UserPrefix, m.styles.Role.UserText)...)
		body := entry.answer
		aPrefix := fmt.Sprintf("  A%d: ", number)
		if strings.TrimSpace(entry.err) != "" {
			body = entry.err
			lines = append(lines, renderWrappedPrefixed(body, aPrefix, strings.Repeat(" ", len(aPrefix)), width, m.styles, m.styles.Role.ErrorPrefix, m.styles.Error)...)
			continue
		}
		if strings.TrimSpace(body) == "" {
			body = "waiting for answer..."
		}
		lines = append(lines, renderMarkdownPrefixed(body, aPrefix, strings.Repeat(" ", len(aPrefix)), width, m.styles, m.styles.Role.AssistantPrefix)...)
	}
	return lines
}

func (m *Model) scrollSideQuestion(delta int) {
	if delta == 0 {
		return
	}
	m.btw.scroll += delta
	maxScroll := m.maxSideQuestionScroll()
	if m.btw.scroll < 0 {
		m.btw.scroll = 0
	}
	if m.btw.scroll > maxScroll {
		m.btw.scroll = maxScroll
	}
}

func (m *Model) scrollSideQuestionToBottom() {
	m.btw.scroll = 0
}

func (m *Model) scrollSideQuestionToTop() {
	m.btw.scroll = m.maxSideQuestionScroll()
}

func (m Model) clampedSideQuestionScroll() int {
	return m.clampedSideQuestionScrollFor(m.maxSideQuestionScroll())
}

func (m Model) clampedSideQuestionScrollFor(maxScroll int) int {
	if m.btw.scroll <= 0 {
		return 0
	}
	if m.btw.scroll > maxScroll {
		return maxScroll
	}
	return m.btw.scroll
}

func (m Model) maxSideQuestionScroll() int {
	width := contentWidth(m.width)
	height := frameHeight(m.height)
	footer := m.sideQuestionFooterLinesWithoutScroll(width)
	if len(footer) > height {
		return 0
	}
	bodyHeight := height - len(footer)
	bodyLines := len(m.sideQuestionBodyLines(width))
	return maxSideQuestionScrollFor(bodyLines, bodyHeight)
}

func maxSideQuestionScrollFor(bodyLines, bodyHeight int) int {
	if bodyLines <= bodyHeight {
		return 0
	}
	return bodyLines - bodyHeight
}

func (m Model) sideQuestionFooterLinesWithoutScroll(width int) []string {
	lines := []string{m.sideQuestionDraftLine(width)}
	help := "Enter ask · PgUp/PgDown scroll · Esc return & clear"
	if strings.TrimSpace(m.btw.latestAnswer()) != "" {
		help += " · Ctrl+Y copy latest"
	}
	if !m.btw.running {
		help += " · Ctrl+U clear"
	}
	lines = append(lines, m.styles.Render(m.styles.Modal.Help, truncateText(help, width)))
	if toast := m.toastView(width); toast != "" {
		lines = append(lines, splitFrameLines(toast)...)
	}
	lines = append(lines, m.sideQuestionStatusLine(width))
	return lines
}

func (m Model) sideQuestionDraftLine(width int) string {
	prefix := "  BTW> "
	if strings.TrimSpace(m.btw.draft) == "" {
		placeholder := "type follow-up..."
		if m.btw.running {
			placeholder = "waiting for answer..."
		}
		return m.styles.Render(m.styles.Input.Prompt, prefix) +
			m.styles.Render(m.styles.Input.Placeholder, truncateText(placeholder, max(1, width-len(prefix))))
	}
	return m.styles.Render(m.styles.Input.Prompt, prefix) +
		m.styles.Render(m.styles.Input.Text, truncateText(m.btw.draft, max(1, width-len(prefix))))
}

func (m Model) sideQuestionCursor(width, y int, visible bool) *tea.Cursor {
	if !visible || y < 0 {
		return nil
	}
	const prompt = "  BTW> "
	textWidth := max(1, width-len(prompt))
	x := inputCursorX(prompt, m.btw.draft, len([]rune(m.btw.draft)), textWidth)
	if cur := m.input.Cursor(); cur != nil {
		out := *cur
		out.Position.X = x
		out.Position.Y = y
		return &out
	}
	cur := tea.NewCursor(x, y)
	cur.Blink = false
	return cur
}

func (m Model) sideQuestionStatusLine(width int) string {
	state := statusIdle
	if m.btw.running {
		state = "answering"
	}
	segments := []statusSegment{
		{label: "view", value: "btw"},
		{label: "model", value: defaultString(m.status.model, "unknown")},
		{label: "state", value: state, semantic: "state"},
	}
	if len(m.btw.entries) > 0 {
		segments = append(segments, statusSegment{label: "q/a", value: fmt.Sprintf("%d", len(m.btw.entries))})
	}
	segments = append(segments, statusSegment{label: "esc", value: "return & clear", semantic: "help"})
	segments = fitSideQuestionStatusSegments(segments, width, m.styles)

	rendered := make([]string, len(segments))
	separator := m.styles.Render(m.styles.SubtleLine, statusSeparatorText(m.styles))
	for i, segment := range segments {
		rendered[i] = m.styles.Render(statusSegmentStyle(segment, m.styles), statusSegmentText(segment))
	}
	return m.styles.Render(m.styles.Status.Bar, strings.Join(rendered, separator))
}

func fitSideQuestionStatusSegments(segments []statusSegment, width int, styles tuitheme.Styles) []statusSegment {
	width = contentWidth(width)
	out := append([]statusSegment(nil), segments...)
	for _, label := range []string{"q/a", "model", "esc"} {
		if statusSegmentsWidth(out, styles) <= width {
			return out
		}
		out = removeStatusSegment(out, label)
	}
	if statusSegmentsWidth(out, styles) <= width {
		return out
	}
	return []statusSegment{{label: "btw"}}
}

func (s sideQuestionState) latestAnswer() string {
	for i := len(s.entries) - 1; i >= 0; i-- {
		if strings.TrimSpace(s.entries[i].answer) != "" {
			return s.entries[i].answer
		}
	}
	return ""
}

func (s sideQuestionState) history() []SideQuestionMessage {
	var out []SideQuestionMessage
	for _, entry := range s.entries {
		question := strings.TrimSpace(entry.question)
		answer := strings.TrimSpace(entry.answer)
		if question == "" || answer == "" || strings.TrimSpace(entry.err) != "" {
			continue
		}
		out = append(out, SideQuestionMessage{
			Question: question,
			Answer:   entry.answer,
		})
	}
	return out
}
