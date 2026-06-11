package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
)

// PermissionRequest is one pending human approval request.
type PermissionRequest struct {
	Request  permission.Request
	Response chan permission.Decision
}

// PermissionBridge converts permission.Asks into TUI messages.
type PermissionBridge struct {
	requests chan PermissionRequest
}

// NewPermissionBridge creates a bridge suitable for wiring into a permission Manager.
func NewPermissionBridge() *PermissionBridge {
	return &PermissionBridge{requests: make(chan PermissionRequest)}
}

// Requests returns the channel consumed by the TUI model.
func (b *PermissionBridge) Requests() <-chan PermissionRequest {
	if b == nil {
		return nil
	}
	return b.requests
}

// Ask implements permission.Asker.
func (b *PermissionBridge) Ask(ctx context.Context, req permission.Request) (permission.Decision, error) {
	if b == nil {
		return "", errors.New("permission bridge is nil")
	}
	pending := PermissionRequest{
		Request:  req,
		Response: make(chan permission.Decision, 1),
	}
	select {
	case b.requests <- pending:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case decision := <-pending.Response:
		return decision, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type permissionRequestMsg struct {
	request PermissionRequest
	ok      bool
}

func waitForPermission(requests <-chan PermissionRequest) tea.Cmd {
	if requests == nil {
		return nil
	}
	return func() tea.Msg {
		request, ok := <-requests
		return permissionRequestMsg{request: request, ok: ok}
	}
}
