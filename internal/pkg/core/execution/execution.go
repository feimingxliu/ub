// Package execution defines session execution modes and mode-level gates.
package execution

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Mode controls which classes of tool calls may proceed.
type Mode string

const (
	ModeWork       Mode = "work"
	ModePlan       Mode = "plan"
	ModeAuto       Mode = "auto"
	ModeFullAccess Mode = "full-access"
)

// ParseMode parses a user/config mode string. Empty means work.
func ParseMode(raw string) (Mode, error) {
	switch mode := Mode(strings.TrimSpace(raw)); mode {
	case "":
		return ModeWork, nil
	case ModeWork, ModePlan, ModeAuto, ModeFullAccess:
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
	if parsed == ModePlan {
		switch risk {
		case tool.RiskWrite:
			return fmt.Errorf("plan mode is read-only: write tools are disabled")
		case tool.RiskExec:
			return fmt.Errorf("plan mode is read-only: exec tools are disabled")
		case tool.RiskNetwork:
			return fmt.Errorf("plan mode is read-only: network tools are disabled")
		}
	}
	return nil
}
