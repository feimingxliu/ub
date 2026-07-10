// Package fs implements the workspace file-system tool group (read, ls,
// glob, write, edit). All tools share a single resolve() helper that
// enforces a strict sandbox: every input path is cleaned and rejected if
// it escapes the workspace root passed to Register.
//
// The write and edit tools implement tool.PreviewableTool. Preview MUST
// NOT mutate disk; the dispatcher calls Preview to render a diff for the
// user and only calls Execute after approval.
//
// Concurrency: the registered tool instances share an immutable root
// string and do not hold per-call state. Concurrent calls are safe at
// the Go-runtime level, but the V1 agent loop serializes tool calls per
// session anyway, so this package does not add file locks.
package fs
