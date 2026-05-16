// Package reasoning defines provider-neutral reasoning controls.
package reasoning

import (
	"fmt"
	"strings"
)

// Effort is the normalized reasoning effort value used across ub.
type Effort string

const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
)

// Config is the runtime/config-file shape for reasoning controls.
type Config struct {
	Effort  Effort `yaml:"effort,omitempty"  json:"effort,omitempty"`
	Summary string `yaml:"summary,omitempty" json:"summary,omitempty"`
}

// NormalizeEffort validates and canonicalizes an effort string.
func NormalizeEffort(value string) (Effort, error) {
	switch effort := Effort(strings.ToLower(strings.TrimSpace(value))); effort {
	case "", EffortNone:
		return EffortNone, nil
	case EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh:
		return effort, nil
	default:
		return "", fmt.Errorf("unknown reasoning effort %q", value)
	}
}

// MustEfforts is a concise helper for built-in tables.
func MustEfforts(values ...Effort) []Effort {
	return append([]Effort(nil), values...)
}

// Contains reports whether list contains effort.
func Contains(list []Effort, effort Effort) bool {
	for _, candidate := range list {
		if candidate == effort {
			return true
		}
	}
	return false
}
