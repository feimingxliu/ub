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

const closeTimeout = 2 * time.Second
