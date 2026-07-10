package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/agent"
)

// AskRequest is one pending structured preference question from the agent.
type AskRequest struct {
	Request  agent.AskRequest
	Response chan agent.AskResponse
}

// AskBridge converts agent.Asks into TUI messages.
type AskBridge struct {
	requests chan AskRequest
}

// NewAskBridge creates a bridge suitable for wiring into agent.Options.
func NewAskBridge() *AskBridge {
	return &AskBridge{requests: make(chan AskRequest)}
}

// Requests returns the channel consumed by the TUI model.
func (b *AskBridge) Requests() <-chan AskRequest {
	if b == nil {
		return nil
	}
	return b.requests
}

// AskUser implements agent.Asker.
func (b *AskBridge) AskUser(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	if b == nil {
		return agent.AskResponse{}, errors.New("ask bridge is nil")
	}
	pending := AskRequest{
		Request:  req,
		Response: make(chan agent.AskResponse, 1),
	}
	select {
	case b.requests <- pending:
	case <-ctx.Done():
		return agent.AskResponse{}, ctx.Err()
	}
	select {
	case resp := <-pending.Response:
		return resp, nil
	case <-ctx.Done():
		return agent.AskResponse{}, ctx.Err()
	}
}

type askRequestMsg struct {
	request AskRequest
	ok      bool
}

func waitForAsk(requests <-chan AskRequest) tea.Cmd {
	if requests == nil {
		return nil
	}
	return func() tea.Msg {
		request, ok := <-requests
		return askRequestMsg{request: request, ok: ok}
	}
}

// askPromptModel renders the ask tool as a step-by-step wizard: one question
// at a time, with Enter advancing to the next (or submitting on the last).
// Each question's option list has a trailing virtual "Other" entry that,
// when chosen, drops into an inline free-form text input.
type askPromptModel struct {
	request   agent.AskRequest
	question  int // current wizard position
	option    int // cursor over real options + virtual Other slot
	selected  [][]bool
	otherText []string // per-question Other draft
	otherMode bool     // current question is in Other text-input sub-mode
}

func newAskPromptModel(req agent.AskRequest) askPromptModel {
	m := askPromptModel{request: req}
	m.selected = make([][]bool, len(req.Questions))
	m.otherText = make([]string, len(req.Questions))
	for i, q := range req.Questions {
		m.selected[i] = make([]bool, len(q.Options)+1) // +1 for virtual Other slot
	}
	return m
}

// otherIndex returns the virtual Other slot index for the given question.
func otherIndex(q agent.AskQuestion) int { return len(q.Options) }

func (m *askPromptModel) currentQuestion() (agent.AskQuestion, bool) {
	if m.question < 0 || m.question >= len(m.request.Questions) {
		return agent.AskQuestion{}, false
	}
	return m.request.Questions[m.question], true
}

func (m askPromptModel) View(width int) string {
	width = contentWidth(width)
	var b strings.Builder
	total := len(m.request.Questions)
	if total == 0 {
		return "Agent question"
	}
	fmt.Fprintf(&b, "Agent question (%d/%d)", m.question+1, total)
	q, ok := m.currentQuestion()
	if !ok {
		return b.String()
	}
	indent := "  "
	// Header.
	header := strings.TrimSpace(q.Header)
	if header == "" {
		header = fmt.Sprintf("Question %d", m.question+1)
	}
	b.WriteString("\n")
	writeWrappedPrefixed(&b, header, indent, width)
	// Question body.
	if question := strings.TrimSpace(q.Question); question != "" {
		b.WriteString("\n")
		writeWrappedPrefixed(&b, question, indent, width)
	}
	// Options (real + virtual Other).
	for oi, opt := range q.Options {
		b.WriteString("\n")
		m.writeOptionLine(&b, q, oi, opt.Label, opt.Description, width)
	}
	// Virtual Other option. Its description shows the finalized Other
	// answer only when we are NOT actively typing it (otherwise the live
	// draft is rendered on its own input line just below, and showing it
	// twice would be redundant).
	b.WriteString("\n")
	otherLabel := "Other (type your own)"
	otherDesc := ""
	if !m.otherMode {
		if text := strings.TrimSpace(m.otherText[m.question]); text != "" {
			otherDesc = text
		}
	}
	m.writeOptionLine(&b, q, otherIndex(q), otherLabel, otherDesc, width)
	// Other draft input line when actively typing.
	if m.otherMode {
		b.WriteString("\n")
		writeOtherDraftLine(&b, m.otherText[m.question], true, width)
	}
	// Help line.
	b.WriteString("\n")
	help := "Enter confirm/next · Space select · Tab/←→ prev-next question · Esc skip"
	writeWrappedPrefixed(&b, help, indent, width)
	return b.String()
}

// writeOptionLine renders one option (real or virtual Other) with a
// cursor/marker prefix, wrapping the label+description to fit width while
// indenting continuation lines under the content.
func (m askPromptModel) writeOptionLine(b *strings.Builder, q agent.AskQuestion, oi int, label, desc string, width int) {
	cursor := "  "
	if oi == m.option {
		cursor = "> "
	}
	marker := "( )"
	if q.MultiSelect {
		marker = "[ ]"
	}
	if m.isSelected(m.question, oi) {
		if q.MultiSelect {
			marker = "[x]"
		} else {
			marker = "(*)"
		}
	}
	num := fmt.Sprintf("%d.", oi+1)
	prefix := cursor + num + " " + marker + " "
	content := strings.TrimSpace(label)
	if desc = strings.TrimSpace(desc); desc != "" {
		content += " - " + desc
	}
	prefixWidth := runewidth.StringWidth(prefix)
	contentWidth := max(10, width-prefixWidth)
	wrapped := wrapLine(content, contentWidth)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	b.WriteString(prefix)
	b.WriteString(wrapped[0])
	for _, line := range wrapped[1:] {
		b.WriteString("\n")
		b.WriteString(strings.Repeat(" ", prefixWidth))
		b.WriteString(line)
	}
}

// writeOtherDraftLine renders the inline Other text input. When active, a
// trailing "_" acts as a pseudo-cursor placed after the final character of
// the draft (we keep frameCursor nil during pendingAsk, so no real terminal
// cursor is shown here).
func writeOtherDraftLine(b *strings.Builder, draft string, active bool, width int) {
	const prefix = "  > your answer: "
	prefixWidth := runewidth.StringWidth(prefix)
	contentWidth := max(10, width-prefixWidth)
	draft = strings.TrimSpace(draft)
	wrapped := wrapLine(draft, contentWidth)
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	last := len(wrapped) - 1
	for i, line := range wrapped {
		if i > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Repeat(" ", prefixWidth))
		} else {
			b.WriteString(prefix)
		}
		b.WriteString(line)
		if active && i == last {
			b.WriteString("_")
		}
	}
}

// writeWrappedPrefixed writes text wrapped to width, with prefix on the first
// line and spaces (matching the prefix visual width) on continuation lines.
func writeWrappedPrefixed(b *strings.Builder, text, prefix string, width int) {
	prefixWidth := runewidth.StringWidth(prefix)
	contentWidth := max(10, width-prefixWidth)
	for i, line := range wrapLine(text, contentWidth) {
		if i > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Repeat(" ", prefixWidth))
		} else {
			b.WriteString(prefix)
		}
		b.WriteString(line)
	}
}

// HandleKey routes a key press through either the Other text-input sub-mode
// or the normal option-navigation execmode.
func (m *askPromptModel) HandleKey(msg tea.KeyPressMsg) {
	if m.otherMode {
		m.handleOtherKey(msg)
		return
	}
	switch msg.String() {
	case "up", "k":
		m.moveOption(-1)
	case "down", "j":
		m.moveOption(1)
	case "left", "shift+tab":
		m.moveQuestion(-1)
	case "right", "tab":
		m.moveQuestion(1)
	case "space":
		m.toggleSelected()
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		idx := int(s[0] - '1')
		if q, ok := m.currentQuestion(); ok && idx < len(q.Options) {
			m.option = idx
			m.toggleSelected()
		}
	}
}

func (m *askPromptModel) handleOtherKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "backspace", "delete":
		m.otherText[m.question] = dropLastRune(m.otherText[m.question])
	case "space":
		if msg.Text == "" {
			m.otherText[m.question] += " "
		} else {
			m.otherText[m.question] += msg.Text
		}
	case "left", "shift+tab", "right", "tab", "up", "down", "k", "j":
		// Navigation keys are ignored while typing so the user can enter
		// arbitrary text including arrows-like input without losing focus.
	default:
		if msg.Text != "" {
			m.otherText[m.question] += msg.Text
		}
	}
}

// HandleEnter processes Enter: it finalizes an in-progress Other draft,
// applies single-select fallback for the current question, then either
// advances to the next question or signals that the whole prompt should be
// submitted. Returns true when the caller should submit.
func (m *askPromptModel) HandleEnter() bool {
	q, ok := m.currentQuestion()
	if !ok {
		return true
	}
	if m.otherMode {
		if strings.TrimSpace(m.otherText[m.question]) == "" {
			// Empty draft: don't advance, stay in input.
			return false
		}
		if q.MultiSelect {
			m.setSelected(m.question, otherIndex(q), true)
		} else {
			m.setSingle(m.question, otherIndex(q))
		}
		m.otherMode = false
	} else if !q.MultiSelect && !m.questionHasSelection(m.question) {
		// Single-select fallback: use the current cursor option (but never
		// the Other slot with an empty draft).
		idx := m.option
		if idx == otherIndex(q) {
			idx = 0
		}
		m.setSingle(m.question, idx)
	}
	if m.question >= len(m.request.Questions)-1 {
		return true
	}
	m.question++
	m.option = 0
	m.otherMode = false
	return false
}

// ExitOtherMode leaves the Other text-input sub-mode without finalizing,
// keeping the draft so the user can resume editing after navigating away.
func (m *askPromptModel) ExitOtherMode() {
	m.otherMode = false
}

func (m *askPromptModel) SubmitResponse() agent.AskResponse {
	for qi, q := range m.request.Questions {
		if q.MultiSelect {
			continue
		}
		if m.questionHasSelection(qi) {
			continue
		}
		if len(q.Options) == 0 {
			continue
		}
		idx := 0
		if qi == m.question && m.option != otherIndex(q) {
			idx = clampInt(m.option, 0, len(q.Options)-1)
		}
		m.setSingle(qi, idx)
	}
	return m.response(false)
}

func (m askPromptModel) SkipResponse() agent.AskResponse {
	return m.response(true)
}

func (m askPromptModel) Summary(resp agent.AskResponse) string {
	if resp.Skipped {
		return "ask skipped"
	}
	var parts []string
	for _, answer := range resp.Answers {
		labelText := ""
		if text := strings.TrimSpace(answer.Text); text != "" {
			labelText = text
		} else {
			var labels []string
			for _, selected := range answer.Selected {
				if label := strings.TrimSpace(selected.Label); label != "" {
					labels = append(labels, label)
				}
			}
			labelText = strings.Join(labels, ", ")
		}
		if labelText == "" {
			labelText = "no selection"
		}
		header := strings.TrimSpace(answer.Header)
		if header == "" {
			parts = append(parts, labelText)
		} else {
			parts = append(parts, header+": "+labelText)
		}
	}
	if len(parts) == 0 {
		return "ask answered"
	}
	return "ask answered: " + strings.Join(parts, "; ")
}

func (m askPromptModel) response(skipped bool) agent.AskResponse {
	resp := agent.AskResponse{Skipped: skipped}
	for qi, q := range m.request.Questions {
		answer := agent.AskAnswer{
			Header:   q.Header,
			Question: q.Question,
			Skipped:  skipped,
		}
		if !skipped && qi < len(m.selected) {
			otherIdx := otherIndex(q)
			for oi, selected := range m.selected[qi] {
				if !selected {
					continue
				}
				if oi == otherIdx {
					if text := strings.TrimSpace(m.otherText[qi]); text != "" {
						answer.Text = text
					}
					continue
				}
				if oi < len(q.Options) {
					answer.Selected = append(answer.Selected, q.Options[oi])
				}
			}
		}
		resp.Answers = append(resp.Answers, answer)
	}
	return resp
}

func (m *askPromptModel) moveOption(delta int) {
	q, ok := m.currentQuestion()
	if !ok {
		return
	}
	count := len(q.Options) + 1 // include virtual Other slot
	if count <= 0 {
		m.option = 0
		return
	}
	m.option = (m.option + delta + count) % count
}

func (m *askPromptModel) moveQuestion(delta int) {
	if len(m.request.Questions) == 0 {
		return
	}
	m.question = (m.question + delta + len(m.request.Questions)) % len(m.request.Questions)
	m.otherMode = false
	q, ok := m.currentQuestion()
	if !ok {
		m.option = 0
		return
	}
	maxOption := len(q.Options) // virtual Other is index len(Options); allow landing on it
	m.option = clampInt(m.option, 0, maxOption)
}

func (m *askPromptModel) toggleSelected() {
	q, ok := m.currentQuestion()
	if !ok || m.question >= len(m.selected) {
		return
	}
	if m.option < 0 || m.option >= len(m.selected[m.question]) {
		return
	}
	if m.option == otherIndex(q) {
		// Entering the Other slot starts the text input. For single-select
		// this is mutually exclusive with real options, so clear them (same
		// as picking any real option would). For multi-select the Other
		// draft coexists with other picks.
		m.otherMode = true
		if q.MultiSelect {
			m.setSelected(m.question, m.option, true)
		} else {
			m.setSingle(m.question, m.option)
		}
		return
	}
	if q.MultiSelect {
		m.selected[m.question][m.option] = !m.selected[m.question][m.option]
		return
	}
	m.setSingle(m.question, m.option)
}

func (m *askPromptModel) setSingle(question, option int) {
	if question < 0 || question >= len(m.selected) {
		return
	}
	for i := range m.selected[question] {
		m.selected[question][i] = i == option
	}
}

func (m *askPromptModel) setSelected(question, option int, value bool) {
	if question < 0 || question >= len(m.selected) {
		return
	}
	if option < 0 || option >= len(m.selected[question]) {
		return
	}
	m.selected[question][option] = value
}

func (m askPromptModel) isSelected(question, option int) bool {
	if question < 0 || question >= len(m.selected) {
		return false
	}
	if option < 0 || option >= len(m.selected[question]) {
		return false
	}
	return m.selected[question][option]
}

func (m askPromptModel) questionHasSelection(question int) bool {
	if question < 0 || question >= len(m.selected) {
		return false
	}
	for _, selected := range m.selected[question] {
		if selected {
			return true
		}
	}
	return false
}
