// Package execution defines session execution modes and mode-level gates.
package execution

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/tool"
)

// Mode controls which classes of tool calls may proceed.
type Mode string

const (
	ModeDefault      Mode = "default"
	ModePlan         Mode = "plan"
	ModeAgentApprove Mode = "agent-approve"
)

// ParseMode parses a user/config mode string. Empty means default.
func ParseMode(raw string) (Mode, error) {
	switch mode := Mode(strings.TrimSpace(raw)); mode {
	case "":
		return ModeDefault, nil
	case ModeDefault, ModePlan, ModeAgentApprove:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown execution mode %q", raw)
	}
}

// Gate applies mode-only policy before permission approval.
func Gate(mode Mode, risk tool.Risk) error {
	parsed, err := ParseMode(string(mode))
	if err != nil {
		return err
	}
	if parsed == ModePlan && risk == tool.RiskWrite {
		return fmt.Errorf("plan mode is read-only: write tools are disabled")
	}
	return nil
}
