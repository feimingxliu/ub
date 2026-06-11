package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
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

type askPromptModel struct {
	request  agent.AskRequest
	question int
	option   int
	selected [][]bool
}

func newAskPromptModel(req agent.AskRequest) askPromptModel {
	m := askPromptModel{request: req}
	m.selected = make([][]bool, len(req.Questions))
	for i, q := range req.Questions {
		m.selected[i] = make([]bool, len(q.Options))
	}
	return m
}

func (m askPromptModel) View(width int) string {
	width = contentWidth(width)
	var b strings.Builder
	b.WriteString("Agent question")
	for qi, q := range m.request.Questions {
		b.WriteString("\n")
		prefix := "  "
		if qi == m.question {
			prefix = "> "
		}
		header := strings.TrimSpace(q.Header)
		if header == "" {
			header = fmt.Sprintf("Question %d", qi+1)
		}
		b.WriteString(prefix)
		b.WriteString(truncateText(header, max(10, width-2)))
		question := strings.TrimSpace(q.Question)
		if question != "" {
			b.WriteString("\n  ")
			b.WriteString(truncateText(question, max(10, width-2)))
		}
		for oi, opt := range q.Options {
			b.WriteString("\n")
			cursor := "  "
			if qi == m.question && oi == m.option {
				cursor = "> "
			}
			marker := "( )"
			if q.MultiSelect {
				marker = "[ ]"
			}
			if qi < len(m.selected) && oi < len(m.selected[qi]) && m.selected[qi][oi] {
				if q.MultiSelect {
					marker = "[x]"
				} else {
					marker = "(*)"
				}
			}
			line := fmt.Sprintf("%s%d. %s %s", cursor, oi+1, marker, strings.TrimSpace(opt.Label))
			if desc := strings.TrimSpace(opt.Description); desc != "" {
				line += " - " + desc
			}
			b.WriteString(truncateText(line, max(10, width)))
		}
	}
	b.WriteString("\n")
	b.WriteString(truncateText("Enter submits  Space selects  Tab changes question  Esc skips", max(10, width)))
	return b.String()
}

func (m *askPromptModel) HandleKey(key string) {
	switch key {
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
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if m.question < len(m.request.Questions) && idx < len(m.request.Questions[m.question].Options) {
			m.option = idx
			m.toggleSelected()
		}
	}
}

func (m *askPromptModel) SubmitResponse() agent.AskResponse {
	for qi, q := range m.request.Questions {
		if q.MultiSelect {
			continue
		}
		if !m.questionHasSelection(qi) && len(q.Options) > 0 {
			idx := 0
			if qi == m.question {
				idx = clampInt(m.option, 0, len(q.Options)-1)
			}
			m.setSingle(qi, idx)
		}
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
		var labels []string
		for _, selected := range answer.Selected {
			if label := strings.TrimSpace(selected.Label); label != "" {
				labels = append(labels, label)
			}
		}
		labelText := strings.Join(labels, ", ")
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
			for oi, selected := range m.selected[qi] {
				if selected && oi < len(q.Options) {
					answer.Selected = append(answer.Selected, q.Options[oi])
				}
			}
		}
		resp.Answers = append(resp.Answers, answer)
	}
	return resp
}

func (m *askPromptModel) moveOption(delta int) {
	if len(m.request.Questions) == 0 {
		return
	}
	q := m.request.Questions[m.question]
	if len(q.Options) == 0 {
		m.option = 0
		return
	}
	m.option = (m.option + delta + len(q.Options)) % len(q.Options)
}

func (m *askPromptModel) moveQuestion(delta int) {
	if len(m.request.Questions) == 0 {
		return
	}
	m.question = (m.question + delta + len(m.request.Questions)) % len(m.request.Questions)
	maxOption := len(m.request.Questions[m.question].Options) - 1
	if maxOption < 0 {
		m.option = 0
		return
	}
	m.option = clampInt(m.option, 0, maxOption)
}

func (m *askPromptModel) toggleSelected() {
	if m.question >= len(m.request.Questions) || m.question >= len(m.selected) {
		return
	}
	q := m.request.Questions[m.question]
	if m.option < 0 || m.option >= len(q.Options) || m.option >= len(m.selected[m.question]) {
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
