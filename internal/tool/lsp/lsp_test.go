package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	lspruntime "github.com/feimingxliu/ub/internal/lsp"
	"github.com/feimingxliu/ub/internal/tool"
)

type fakeManager struct {
	diagnostics []lspruntime.FileDiagnostics
	locations   []lspruntime.Location
	refFile     string
	refLine     int
	refCol      int
	symbol      string
	symbolPath  string
}

func (m *fakeManager) Diagnostics(context.Context, string) ([]lspruntime.FileDiagnostics, error) {
	return m.diagnostics, nil
}

func (m *fakeManager) References(_ context.Context, file string, line, col int) ([]lspruntime.Location, error) {
	m.refFile = file
	m.refLine = line
	m.refCol = col
	return m.locations, nil
}

func (m *fakeManager) ReferencesBySymbol(_ context.Context, symbol, path string) ([]lspruntime.Location, error) {
	m.symbol = symbol
	m.symbolPath = path
	return m.locations, nil
}

func TestDiagnosticsToolFormatsDiagnosticsAndNoDiagnostics(t *testing.T) {
	m := &fakeManager{diagnostics: []lspruntime.FileDiagnostics{{
		Path: "main.go",
		Diagnostics: []lspruntime.Diagnostic{{
			Range:    lspruntime.Range{Start: lspruntime.Position{Line: 2, Character: 4}},
			Severity: 1,
			Message:  "expected ';'",
			Source:   "gopls",
		}},
	}}}
	tl := newDiagnosticsTool(m)
	res, err := execTool(t, tl, diagnosticsArgs{File: "main.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Content, "main.go:3:5: error: expected ';' [gopls]") {
		t.Fatalf("unexpected diagnostics output:\n%s", res.Content)
	}

	m.diagnostics = nil
	res, err = execTool(t, tl, diagnosticsArgs{})
	if err != nil {
		t.Fatalf("Execute no diagnostics: %v", err)
	}
	if res.Content != "no diagnostics" {
		t.Fatalf("content = %q, want no diagnostics", res.Content)
	}
}

func TestReferencesToolFormatsLocationsAndNoReferences(t *testing.T) {
	m := &fakeManager{locations: []lspruntime.Location{{
		URI:   "file:///tmp/work/main.go",
		Range: lspruntime.Range{Start: lspruntime.Position{Line: 9, Character: 2}},
	}}}
	tl := newReferencesTool(m)
	res, err := execTool(t, tl, referencesArgs{File: "main.go", Line: 1, Col: 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Content, "/tmp/work/main.go:10:3") {
		t.Fatalf("unexpected references output:\n%s", res.Content)
	}

	m.locations = nil
	res, err = execTool(t, tl, referencesArgs{File: "main.go", Line: 1, Col: 1})
	if err != nil {
		t.Fatalf("Execute no references: %v", err)
	}
	if res.Content != "no references" {
		t.Fatalf("content = %q, want no references", res.Content)
	}
}

func TestReferencesToolAcceptsNumericStringPosition(t *testing.T) {
	m := &fakeManager{locations: []lspruntime.Location{{
		URI:   "file:///tmp/work/main.go",
		Range: lspruntime.Range{Start: lspruntime.Position{Line: 0, Character: 0}},
	}}}
	tl := newReferencesTool(m)
	res, err := tl.Execute(context.Background(), json.RawMessage(`{"file":"main.go","line":"38","col":"11"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Content == "no references" {
		t.Fatalf("expected references, got %q", res.Content)
	}
	if m.refFile != "main.go" || m.refLine != 38 || m.refCol != 11 {
		t.Fatalf("references args = %q %d %d, want main.go 38 11", m.refFile, m.refLine, m.refCol)
	}
}

func TestReferencesToolPrefersSymbolLookup(t *testing.T) {
	m := &fakeManager{locations: []lspruntime.Location{{
		URI:   "file:///tmp/work/main.go",
		Range: lspruntime.Range{Start: lspruntime.Position{Line: 0, Character: 0}},
	}}}
	tl := newReferencesTool(m)
	_, err := tl.Execute(context.Background(), json.RawMessage(`{"symbol":"References","file":"internal/lsp/manager.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if m.symbol != "References" || m.symbolPath != "internal/lsp/manager.go" {
		t.Fatalf("symbol lookup = %q in %q, want References in internal/lsp/manager.go", m.symbol, m.symbolPath)
	}
	if m.refFile != "" || m.refLine != 0 || m.refCol != 0 {
		t.Fatalf("position lookup should not run, got %q %d %d", m.refFile, m.refLine, m.refCol)
	}
}

func TestRegisterAddsLSPTools(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, &fakeManager{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, name := range []string{"diagnostics", "references"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
}

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tl.Execute(context.Background(), raw)
}
