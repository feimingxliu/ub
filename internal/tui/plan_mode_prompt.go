package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/agent"
	execmode "github.com/feimingxliu/ub/internal/mode"
)

// PlanModeRequest is one pending model-requested plan-mode transition.
type PlanModeRequest struct {
	Request  agent.PlanModeRequest
	Response chan agent.PlanModeResponse
}

// PlanModeBridge converts agent plan-mode requests into TUI messages.
type PlanModeBridge struct {
	requests chan PlanModeRequest
}

// NewPlanModeBridge creates a bridge suitable for wiring into agent.Options.
func NewPlanModeBridge() *PlanModeBridge {
	return &PlanModeBridge{requests: make(chan PlanModeRequest)}
}

// Requests returns the channel consumed by the TUI model.
func (b *PlanModeBridge) Requests() <-chan PlanModeRequest {
	if b == nil {
		return nil
	}
	return b.requests
}

// ConfirmPlanMode implements agent.PlanModeController.
func (b *PlanModeBridge) ConfirmPlanMode(ctx context.Context, req agent.PlanModeRequest) (agent.PlanModeResponse, error) {
	if b == nil {
		return agent.PlanModeResponse{}, errors.New("plan mode bridge is nil")
	}
	pending := PlanModeRequest{
		Request:  req,
		Response: make(chan agent.PlanModeResponse, 1),
	}
	select {
	case b.requests <- pending:
	case <-ctx.Done():
		return agent.PlanModeResponse{}, ctx.Err()
	}
	select {
	case resp := <-pending.Response:
		return resp, nil
	case <-ctx.Done():
		return agent.PlanModeResponse{}, ctx.Err()
	}
}

type planModeRequestMsg struct {
	request PlanModeRequest
	ok      bool
}

func waitForPlanMode(requests <-chan PlanModeRequest) tea.Cmd {
	if requests == nil {
		return nil
	}
	return func() tea.Msg {
		request, ok := <-requests
		return planModeRequestMsg{request: request, ok: ok}
	}
}

type planModePromptModel struct {
	request agent.PlanModeRequest
}

func newPlanModePromptModel(req agent.PlanModeRequest) planModePromptModel {
	return planModePromptModel{request: req}
}

func (m planModePromptModel) View(width int) string {
	width = contentWidth(width)
	var b strings.Builder
	switch m.request.Action {
	case agent.PlanModeExit:
		b.WriteString("Exit plan mode?")
		if planID := strings.TrimSpace(m.request.PlanID); planID != "" {
			b.WriteString("\nPlan: ")
			b.WriteString(truncateText(planID, max(10, width-6)))
		}
		if summary := strings.TrimSpace(m.request.Summary); summary != "" {
			b.WriteString("\n")
			b.WriteString(truncateText(summary, max(10, width)))
		}
	default:
		b.WriteString("Enter plan mode?")
		if reason := strings.TrimSpace(m.request.Reason); reason != "" {
			b.WriteString("\n")
			b.WriteString(truncateText(reason, max(10, width)))
		}
	}
	b.WriteString("\n\nEnter/y approves, Esc/n denies")
	return b.String()
}

func (m planModePromptModel) Response(approved bool, from, to string, err error) agent.PlanModeResponse {
	resp := agent.PlanModeResponse{Approved: approved}
	if parsed, ok := parsePromptMode(from); ok {
		resp.FromMode = parsed
	}
	if parsed, ok := parsePromptMode(to); ok {
		resp.ToMode = parsed
	}
	if err != nil {
		resp.Approved = false
		resp.Reason = err.Error()
		return resp
	}
	if !approved {
		switch m.request.Action {
		case agent.PlanModeExit:
			resp.Reason = "user requested plan revision"
		default:
			resp.Reason = "user chose to continue without plan mode"
		}
	}
	return resp
}

func (m planModePromptModel) Summary(resp agent.PlanModeResponse) string {
	action := "plan mode"
	switch m.request.Action {
	case agent.PlanModeExit:
		action = "exit plan mode"
	case agent.PlanModeEnter:
		action = "enter plan mode"
	}
	if resp.Approved {
		return fmt.Sprintf("approved %s", action)
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return fmt.Sprintf("declined %s: %s", action, resp.Reason)
	}
	return fmt.Sprintf("declined %s", action)
}

func parsePromptMode(raw string) (execmode.Mode, bool) {
	mode, err := execmode.ParseMode(raw)
	return mode, err == nil
}
