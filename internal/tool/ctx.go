package tool

import "context"

// ctxKey is a private type so that other packages cannot collide with these
// context keys.
type ctxKey int

const (
	sessionIDKey ctxKey = iota
)

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
