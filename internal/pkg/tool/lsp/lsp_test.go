package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	lspruntime "github.com/feimingxliu/ub/internal/pkg/integration/lsp"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type fakeManager struct {
	diagnostics []lspruntime.FileDiagnostics
	locations   []lspruntime.Location
	refFile     string
	refLine     int
	refCol      int
	symbol      string
	symbolPath  string

	hover           lspruntime.HoverResult
	completion      []lspruntime.CompletionItem
	completionMax   int
	docSymbols      []lspruntime.DocumentSymbol
	renameEdit      lspruntime.WorkspaceEdit
	renameNewName   string
	codeActions     []lspruntime.CodeAction
	codeActionRange [4]int // line, col, endLine, endCol
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

func (m *fakeManager) Hover(_ context.Context, _ string, _, _ int) (lspruntime.HoverResult, error) {
	return m.hover, nil
}

func (m *fakeManager) Completion(_ context.Context, _ string, _, _, maxN int) ([]lspruntime.CompletionItem, error) {
	m.completionMax = maxN
	items := m.completion
	if maxN > 0 && len(items) > maxN {
		items = items[:maxN]
	}
	return items, nil
}

func (m *fakeManager) DocumentSymbols(_ context.Context, _ string) ([]lspruntime.DocumentSymbol, error) {
	return m.docSymbols, nil
}

func (m *fakeManager) Rename(_ context.Context, _ string, _, _ int, newName string) (lspruntime.WorkspaceEdit, error) {
	m.renameNewName = newName
	return m.renameEdit, nil
}

func (m *fakeManager) CodeActions(_ context.Context, _ string, line, col, endLine, endCol int) ([]lspruntime.CodeAction, error) {
	m.codeActionRange = [4]int{line, col, endLine, endCol}
	return m.codeActions, nil
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
	for _, name := range []string{"diagnostics", "references", "hover", "completion", "document_symbols", "rename", "code_action"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestRegister_NilManagerSkips(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, nil); err != nil {
		t.Fatalf("Register(nil): %v", err)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("nil manager should register no tools, got %d", len(reg.All()))
	}
}

func TestHoverTool(t *testing.T) {
	m := &fakeManager{hover: lspruntime.HoverResult{Contents: "func foo()"}}
	tl := newHoverTool(m)
	res, err := execTool(t, tl, hoverArgs{File: "x.go", Line: 1, Col: 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Content != "func foo()" {
		t.Fatalf("content = %q", res.Content)
	}
	m.hover = lspruntime.HoverResult{}
	res, _ = execTool(t, tl, hoverArgs{File: "x.go", Line: 1, Col: 1})
	if res.Content != "no hover" {
		t.Fatalf("expected no hover, got %q", res.Content)
	}
}

func TestCompletionTool_DefaultsAndCap(t *testing.T) {
	m := &fakeManager{}
	for i := 0; i < 150; i++ {
		m.completion = append(m.completion, lspruntime.CompletionItem{Label: "x"})
	}
	tl := newCompletionTool(m)
	_, err := execTool(t, tl, completionArgs{File: "x.go", Line: 1, Col: 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if m.completionMax != defaultCompletionMax {
		t.Fatalf("default max = %d, want %d", m.completionMax, defaultCompletionMax)
	}
	_, _ = execTool(t, tl, completionArgs{File: "x.go", Line: 1, Col: 1, Max: 500})
	if m.completionMax != maxCompletionItems {
		t.Fatalf("cap not enforced: max = %d", m.completionMax)
	}
}

func TestCompletionTool_FormatsLabelDetail(t *testing.T) {
	m := &fakeManager{completion: []lspruntime.CompletionItem{
		{Label: "Println", Detail: "func(args ...any)"},
		{Label: "Print", Detail: ""},
	}}
	tl := newCompletionTool(m)
	res, err := execTool(t, tl, completionArgs{File: "x.go", Line: 1, Col: 1, Max: 10})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "Println\tfunc(args ...any)") {
		t.Fatalf("content missing label+detail:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Print\t") {
		t.Fatalf("content missing label-only row:\n%s", res.Content)
	}
}

func TestDocumentSymbolsTool_Indented(t *testing.T) {
	m := &fakeManager{docSymbols: []lspruntime.DocumentSymbol{
		{
			Name: "Foo", Kind: 23,
			Range: lspruntime.Range{Start: lspruntime.Position{Line: 0}, End: lspruntime.Position{Line: 9, Character: 0}},
			Children: []lspruntime.DocumentSymbol{
				{Name: "Bar", Kind: 6, Range: lspruntime.Range{Start: lspruntime.Position{Line: 1, Character: 1}, End: lspruntime.Position{Line: 2}}},
			},
		},
	}}
	tl := newDocumentSymbolsTool(m)
	res, err := execTool(t, tl, documentSymbolsArgs{File: "x.go"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "Struct Foo") {
		t.Fatalf("missing struct line:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "  Method Bar") {
		t.Fatalf("missing indented method:\n%s", res.Content)
	}
}

func TestRenameTool_FormatsEdits(t *testing.T) {
	m := &fakeManager{renameEdit: lspruntime.WorkspaceEdit{Edits: []lspruntime.TextEdit{
		{Path: "/tmp/a.go", URI: "file:///tmp/a.go", NewText: "Baz", Range: lspruntime.Range{Start: lspruntime.Position{Line: 0, Character: 4}}},
		{Path: "/tmp/b.go", URI: "file:///tmp/b.go", NewText: "Baz", Range: lspruntime.Range{Start: lspruntime.Position{Line: 9, Character: 0}}},
	}}}
	tl := newRenameTool(m)
	res, err := execTool(t, tl, renameArgs{File: "/tmp/a.go", Line: 1, Col: 5, NewName: "Baz"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "Apply via multiedit") {
		t.Fatalf("missing multiedit hint:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "/tmp/a.go:1:5 → 'Baz'") || !strings.Contains(res.Content, "/tmp/b.go:10:1 → 'Baz'") {
		t.Fatalf("edits malformed:\n%s", res.Content)
	}
	if m.renameNewName != "Baz" {
		t.Fatalf("newName not forwarded: %q", m.renameNewName)
	}
}

func TestRenameTool_EmptyNewNameRejected(t *testing.T) {
	tl := newRenameTool(&fakeManager{})
	_, err := execTool(t, tl, renameArgs{File: "x.go", Line: 1, Col: 1, NewName: ""})
	if err == nil || !strings.Contains(err.Error(), "new_name is required") {
		t.Fatalf("expected new_name error, got: %v", err)
	}
}

func TestCodeActionTool_ListsActions(t *testing.T) {
	m := &fakeManager{codeActions: []lspruntime.CodeAction{
		{Title: "Add import", Kind: "quickfix", HasEdit: true},
		{Title: "Extract function", Kind: "refactor.extract"},
	}}
	tl := newCodeActionTool(m)
	res, err := execTool(t, tl, codeActionArgs{File: "x.go", Line: 3, Col: 5})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "Add import (quickfix) — has_edit") {
		t.Fatalf("missing first action:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Extract function (refactor.extract)") {
		t.Fatalf("missing second action:\n%s", res.Content)
	}
	// endLine/endCol default to line/col.
	if m.codeActionRange != [4]int{3, 5, 3, 5} {
		t.Fatalf("range = %v, want [3 5 3 5]", m.codeActionRange)
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
