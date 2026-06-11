package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	lspruntime "github.com/feimingxliu/ub/internal/pkg/integration/lsp"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// ----- hover -----

type hoverArgs struct {
	File string      `json:"file" jsonschema:"required,description=Workspace file path."`
	Line tool.IntArg `json:"line" jsonschema:"required,description=1-based line number."`
	Col  tool.IntArg `json:"col"  jsonschema:"required,description=1-based column number."`
}

type hoverTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newHoverTool(m Manager) *hoverTool {
	return &hoverTool{manager: m, schema: jsonschema.Reflect(&hoverArgs{})}
}

func (t *hoverTool) Name() string { return "hover" }
func (t *hoverTool) Description() string {
	return "Read the LSP hover (signature, doc comment, type info) for the symbol at a 1-based file position."
}
func (t *hoverTool) Schema() *jsonschema.Schema { return t.schema }
func (t *hoverTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *hoverTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a hoverArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("hover: invalid args: %w", err)
	}
	res, err := t.manager.Hover(ctx, a.File, int(a.Line), int(a.Col))
	if err != nil {
		return tool.Result{}, err
	}
	text := strings.TrimSpace(res.Contents)
	if text == "" {
		text = "no hover"
	}
	return tool.Result{Content: text}, nil
}

// ----- completion -----

const (
	defaultCompletionMax = 25
	maxCompletionItems   = 100
)

type completionArgs struct {
	File string      `json:"file" jsonschema:"required,description=Workspace file path."`
	Line tool.IntArg `json:"line" jsonschema:"required,description=1-based line number."`
	Col  tool.IntArg `json:"col"  jsonschema:"required,description=1-based column number."`
	Max  tool.IntArg `json:"max,omitempty" jsonschema:"description=Maximum number of items (default 25, capped at 100)."`
}

type completionTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newCompletionTool(m Manager) *completionTool {
	return &completionTool{manager: m, schema: jsonschema.Reflect(&completionArgs{})}
}

func (t *completionTool) Name() string { return "completion" }
func (t *completionTool) Description() string {
	return "Ask the language server for completion suggestions at a 1-based file position. Returns up to `max` items (default 25, hard cap 100)."
}
func (t *completionTool) Schema() *jsonschema.Schema { return t.schema }
func (t *completionTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *completionTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a completionArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("completion: invalid args: %w", err)
	}
	maxN := int(a.Max)
	if maxN <= 0 {
		maxN = defaultCompletionMax
	}
	if maxN > maxCompletionItems {
		maxN = maxCompletionItems
	}
	items, err := t.manager.Completion(ctx, a.File, int(a.Line), int(a.Col), maxN)
	if err != nil {
		return tool.Result{}, err
	}
	if len(items) == 0 {
		return tool.Result{Content: "no completions"}, nil
	}
	var b strings.Builder
	for _, it := range items {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(it.Label)
		b.WriteByte('\t')
		b.WriteString(it.Detail)
	}
	return tool.Result{Content: b.String()}, nil
}

// ----- document_symbols -----

type documentSymbolsArgs struct {
	File string `json:"file" jsonschema:"required,description=Workspace file path."`
}

type documentSymbolsTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newDocumentSymbolsTool(m Manager) *documentSymbolsTool {
	return &documentSymbolsTool{manager: m, schema: jsonschema.Reflect(&documentSymbolsArgs{})}
}

func (t *documentSymbolsTool) Name() string { return "document_symbols" }
func (t *documentSymbolsTool) Description() string {
	return "List the symbols defined in one file as an indented tree (name, kind, range)."
}
func (t *documentSymbolsTool) Schema() *jsonschema.Schema { return t.schema }
func (t *documentSymbolsTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *documentSymbolsTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a documentSymbolsArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("document_symbols: invalid args: %w", err)
	}
	if strings.TrimSpace(a.File) == "" {
		return tool.Result{}, fmt.Errorf("document_symbols: file is required")
	}
	symbols, err := t.manager.DocumentSymbols(ctx, a.File)
	if err != nil {
		return tool.Result{}, err
	}
	if len(symbols) == 0 {
		return tool.Result{Content: "no symbols"}, nil
	}
	var b strings.Builder
	renderSymbols(&b, symbols, 0)
	return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

func renderSymbols(b *strings.Builder, syms []lspruntime.DocumentSymbol, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, s := range syms {
		fmt.Fprintf(
			b, "%s%s %s [%d:%d-%d:%d]\n",
			indent,
			symbolKindName(s.Kind),
			s.Name,
			s.Range.Start.Line+1, s.Range.Start.Character+1,
			s.Range.End.Line+1, s.Range.End.Character+1,
		)
		if len(s.Children) > 0 {
			renderSymbols(b, s.Children, depth+1)
		}
	}
}

// symbolKindName follows the LSP SymbolKind enum (1..26). Unknown values
// stringify as "Symbol".
func symbolKindName(k int) string {
	names := []string{
		"", "File", "Module", "Namespace", "Package", "Class", "Method",
		"Property", "Field", "Constructor", "Enum", "Interface", "Function",
		"Variable", "Constant", "String", "Number", "Boolean", "Array",
		"Object", "Key", "Null", "EnumMember", "Struct", "Event", "Operator",
		"TypeParameter",
	}
	if k >= 1 && k < len(names) {
		return names[k]
	}
	return "Symbol"
}

// ----- rename -----

type renameArgs struct {
	File    string      `json:"file"     jsonschema:"required,description=Workspace file path containing the symbol to rename."`
	Line    tool.IntArg `json:"line"     jsonschema:"required,description=1-based line of the symbol."`
	Col     tool.IntArg `json:"col"      jsonschema:"required,description=1-based column inside the symbol."`
	NewName string      `json:"new_name" jsonschema:"required,description=New identifier."`
}

type renameTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newRenameTool(m Manager) *renameTool {
	return &renameTool{manager: m, schema: jsonschema.Reflect(&renameArgs{})}
}

func (t *renameTool) Name() string { return "rename" }
func (t *renameTool) Description() string {
	return "Ask the language server which edits a rename would produce. Returns the suggested edits as a list; this tool does NOT write to disk — apply the edits via the multiedit tool."
}
func (t *renameTool) Schema() *jsonschema.Schema { return t.schema }
func (t *renameTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *renameTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a renameArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("rename: invalid args: %w", err)
	}
	if strings.TrimSpace(a.NewName) == "" {
		return tool.Result{}, fmt.Errorf("rename: new_name is required")
	}
	edit, err := t.manager.Rename(ctx, a.File, int(a.Line), int(a.Col), a.NewName)
	if err != nil {
		return tool.Result{}, err
	}
	if len(edit.Edits) == 0 {
		return tool.Result{Content: "no rename edits"}, nil
	}
	var b strings.Builder
	b.WriteString("Rename suggested by LSP. Apply via multiedit:\n")
	for _, e := range edit.Edits {
		fmt.Fprintf(
			&b, "- %s:%d:%d → '%s'\n",
			displayPath(e.Path),
			e.Range.Start.Line+1, e.Range.Start.Character+1,
			e.NewText,
		)
	}
	return tool.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

// ----- code_action -----

type codeActionArgs struct {
	File    string      `json:"file"     jsonschema:"required,description=Workspace file path."`
	Line    tool.IntArg `json:"line"     jsonschema:"required,description=1-based line number."`
	Col     tool.IntArg `json:"col"      jsonschema:"required,description=1-based column number."`
	EndLine tool.IntArg `json:"end_line,omitempty" jsonschema:"description=Optional 1-based end line (defaults to line)."`
	EndCol  tool.IntArg `json:"end_col,omitempty"  jsonschema:"description=Optional 1-based end column (defaults to col)."`
}

type codeActionTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newCodeActionTool(m Manager) *codeActionTool {
	return &codeActionTool{manager: m, schema: jsonschema.Reflect(&codeActionArgs{})}
}

func (t *codeActionTool) Name() string { return "code_action" }
func (t *codeActionTool) Description() string {
	return "List code actions available at the given range (quickfixes, refactors, source actions). This tool reports only titles and kinds; it does not execute any action."
}
func (t *codeActionTool) Schema() *jsonschema.Schema { return t.schema }
func (t *codeActionTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *codeActionTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a codeActionArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("code_action: invalid args: %w", err)
	}
	endLine := int(a.EndLine)
	if endLine <= 0 {
		endLine = int(a.Line)
	}
	endCol := int(a.EndCol)
	if endCol <= 0 {
		endCol = int(a.Col)
	}
	actions, err := t.manager.CodeActions(ctx, a.File, int(a.Line), int(a.Col), endLine, endCol)
	if err != nil {
		return tool.Result{}, err
	}
	if len(actions) == 0 {
		return tool.Result{Content: "no code actions"}, nil
	}
	var b strings.Builder
	for _, ac := range actions {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		kind := ac.Kind
		if kind == "" {
			kind = "action"
		}
		fmt.Fprintf(&b, "%s (%s)", ac.Title, kind)
		if ac.HasEdit {
			b.WriteString(" — has_edit")
		}
	}
	return tool.Result{Content: b.String()}, nil
}
