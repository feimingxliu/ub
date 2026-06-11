// Package lsp implements ub's minimal Language Server Protocol client.
package lsp

import "time"

// ServerConfig configures one stdio LSP server process.
type ServerConfig struct {
	Command   string
	Args      []string
	Env       map[string]string
	Root      string
	FileTypes []string
}

// Position is an LSP position using zero-based line and character values.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is an LSP text range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic mirrors the LSP diagnostic fields ub exposes to tools.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// Location is an LSP location.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// FileDiagnostics groups diagnostics for one file.
type FileDiagnostics struct {
	URI         string
	Path        string
	Diagnostics []Diagnostic
}

// HoverResult is the normalized form of a textDocument/hover response.
// The raw LSP shape can be MarkupContent, MarkedString, or MarkedString[];
// the client flattens all three into a single plain-text/markdown string.
type HoverResult struct {
	Contents string
	Range    *Range
}

// CompletionItem is a single completion suggestion.
type CompletionItem struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Kind   int    `json:"kind,omitempty"`
}

// DocumentSymbol is a recursive symbol tree node from textDocument/documentSymbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// TextEdit is one edit produced by rename/code_action.
type TextEdit struct {
	URI     string `json:"-"` // path-side; populated from the surrounding WorkspaceEdit
	Path    string `json:"-"`
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit aggregates rename / code-action edits across files.
type WorkspaceEdit struct {
	Edits []TextEdit
}

// CodeAction is a single available code action from textDocument/codeAction.
// HasEdit reports whether the action carries a WorkspaceEdit payload (vs a
// pure Command); ub does not currently apply either form.
type CodeAction struct {
	Title   string `json:"title"`
	Kind    string `json:"kind,omitempty"`
	HasEdit bool   `json:"-"`
}

const closeTimeout = 2 * time.Second
