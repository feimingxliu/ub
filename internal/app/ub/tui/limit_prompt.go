package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
)

// limitPromptView renders the inline confirmation prompt shown when the
// agent has hit max-turns and is asking whether to keep going. Returns
// the empty string when no prompt is pending.
func (m Model) limitPromptView(width int) string {
	if m.pendingLimit == nil {
		return ""
	}
	width = contentWidth(width)
	title := m.styles.Render(m.styles.Status.Label, "▌ Agent reached the tool-loop cap")
	body := fmt.Sprintf("Used %d turns. Give it %d more, or finalize without tools?",
		m.pendingLimit.Request.UsedTurns, defaultLimitExtension)
	hint := m.styles.Render(m.styles.Status.Bar, "  [y] continue   [n] finalize   (Enter = yes, Esc = no)")
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n  ")
	b.WriteString(truncateText(body, max(10, width-2)))
	b.WriteString("\n")
	b.WriteString(hint)
	return b.String()
}

// defaultLimitExtension is the size of one approved "give it more turns"
// burst when an explicit max_turns guard is configured.
const defaultLimitExtension = 50

// LimitRequest is one pending host-side decision about whether to extend
// the agent loop past max_turns.
type LimitRequest struct {
	Request  agent.LimitExtensionRequest
	Response chan agent.LimitExtensionResponse
}

// LimitBridge converts agent.LimitAsker calls into TUI messages, mirroring
// the permission.Asker bridge.
type LimitBridge struct {
	requests chan LimitRequest
}

// NewLimitBridge creates a bridge suitable for wiring into agent.Options.
func NewLimitBridge() *LimitBridge {
	return &LimitBridge{requests: make(chan LimitRequest)}
}

// Requests returns the channel consumed by the TUI model.
func (b *LimitBridge) Requests() <-chan LimitRequest {
	if b == nil {
		return nil
	}
	return b.requests
}

// AskExtension implements agent.LimitAsker.
func (b *LimitBridge) AskExtension(ctx context.Context, req agent.LimitExtensionRequest) (agent.LimitExtensionResponse, error) {
	if b == nil {
		return agent.LimitExtensionResponse{}, errors.New("limit bridge is nil")
	}
	pending := LimitRequest{
		Request:  req,
		Response: make(chan agent.LimitExtensionResponse, 1),
	}
	select {
	case b.requests <- pending:
	case <-ctx.Done():
		return agent.LimitExtensionResponse{}, ctx.Err()
	}
	select {
	case resp := <-pending.Response:
		return resp, nil
	case <-ctx.Done():
		return agent.LimitExtensionResponse{}, ctx.Err()
	}
}

type limitRequestMsg struct {
	request LimitRequest
	ok      bool
}

func waitForLimit(requests <-chan LimitRequest) tea.Cmd {
	if requests == nil {
		return nil
	}
	return func() tea.Msg {
		request, ok := <-requests
		return limitRequestMsg{request: request, ok: ok}
	}
}
