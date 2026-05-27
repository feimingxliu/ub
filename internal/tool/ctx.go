package tool

import "context"

// ctxKey is a private type so that other packages cannot collide with these
// context keys.
type ctxKey int

const (
	sessionIDKey ctxKey = iota
	subagentRunnerKey
	subagentDepthKey
)

// SubagentRunner is implemented by the agent runtime layer to dispatch a
// child agent run for one prompt. The `task` tool calls into it via the
// ctx helpers below so the tool package does not import `agent`.
type SubagentRunner interface {
	RunSubagent(ctx context.Context, prompt string, maxTurns int) (string, error)
}

// WithSubagentRunner returns a child context that carries runner. A nil
// runner is dropped: callers can blindly forward an unset runner without
// branching, and tools see "no runner" instead of a typed-nil interface.
func WithSubagentRunner(ctx context.Context, runner SubagentRunner) context.Context {
	if runner == nil {
		return ctx
	}
	return context.WithValue(ctx, subagentRunnerKey, runner)
}

// SubagentRunnerFromContext returns the previously installed runner, or
// nil if none.
func SubagentRunnerFromContext(ctx context.Context) SubagentRunner {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(subagentRunnerKey).(SubagentRunner)
	return v
}

// WithSubagentDepth marks how many levels of nested task tool calls have
// stacked up on this ctx so far. The task tool uses this to reject
// recursive sub-agents beyond the depth budget.
func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subagentDepthKey, depth)
}

// SubagentDepthFromContext returns the current depth, defaulting to 0
// (= root agent) when no value has been installed.
func SubagentDepthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	v, _ := ctx.Value(subagentDepthKey).(int)
	return v
}

// WithSessionID returns a child context that carries the agent session id.
// Empty session ids are dropped: callers can blindly call this without
// branching, and consumers will simply see "no session id" instead of an
// empty string they need to special-case.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext returns the session id previously installed by
// WithSessionID, or "" if none.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionIDKey).(string)
	return v
}
