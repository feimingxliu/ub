package agent

import (
	"errors"
	"fmt"
)

// ErrMaxTurns is returned when a run exceeds its provider/tool loop limit.
var ErrMaxTurns = errors.New("agent: max turns reached")

const maxTurnsFinalInstruction = "Tool iteration limit reached for this turn. Do not call tools. Answer the user's request now using the information already gathered. If the available information is incomplete, say what is missing concisely."

// maxOutputTokensRecoveryLimit caps how many recovery attempts are made when
// the model exhausts its output budget on reasoning and produces no visible
// reply or tool call. Each attempt injects a meta user message asking the
// model to pick up where it was cut off.
const maxOutputTokensRecoveryLimit = 3

const outputTokensRecoveryInstruction = "Output token limit hit. Resume directly — no apology, no recap of what you were doing. Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces."

// emptyResponseError builds the error returned when a provider stream
// completes with neither a text/tool-use reply nor a usable assistant message.
// The common cause is a reasoning model consuming its entire output budget on
// chain-of-thought tokens and hitting finish_reason=length before any reply.
// Without surfacing it, the TUI silently goes idle and looks crashed.
func emptyResponseError(reasoningLen int) error {
	if reasoningLen > 0 {
		return fmt.Errorf("model produced %d chars of reasoning but no reply or tool call (likely hit max_output_tokens during reasoning — lower reasoning effort or raise the model's output limit)", reasoningLen)
	}
	return errors.New("model produced no reply (empty stream — no text, no tool calls, no reasoning)")
}
