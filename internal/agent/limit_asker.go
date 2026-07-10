package agent

import "context"

// LimitAsker lets the agent pause when a tool-loop run hits the max-turns
// cap and ask the host for permission to keep going. Without it the loop
// silently falls through to finalizeWithoutTools, which is awkward for
// reasoning models that still want to call tools.
type LimitAsker interface {
	// AskExtension blocks until the host responds. Return ExtraTurns > 0 to
	// extend the loop; return 0 (or zero-value response) to fall through to
	// the no-tool finalize path. An error aborts the run.
	AskExtension(ctx context.Context, req LimitExtensionRequest) (LimitExtensionResponse, error)
}

// LimitExtensionRequest describes the limit-extension prompt.
type LimitExtensionRequest struct {
	SessionID string
	UserTurn  int
	UsedTurns int
}

// LimitExtensionResponse carries the host's decision.
type LimitExtensionResponse struct {
	ExtraTurns int
}
