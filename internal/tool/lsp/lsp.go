// Package lsp exposes language-server queries as ub tools.
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/invopop/jsonschema"

	lspruntime "github.com/feimingxliu/ub/internal/lsp"
	"github.com/feimingxliu/ub/internal/tool"
)

const defaultLSPToolTimeout = 10 * time.Second

func lspToolContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultLSPToolTimeout)
}

// Manager is the LSP query surface used by these tools. The interface grew
// from the original 3 read methods to 8 when hover / completion /
// document_symbols / rename / code_actions were added; any new
// implementation must cover all 8.
type Manager interface {
	Diagnostics(ctx context.Context, file string) ([]lspruntime.FileDiagnostics, error)
	References(ctx context.Context, file string, line, col int) ([]lspruntime.Location, error)
	ReferencesBySymbol(ctx context.Context, symbol, path string) ([]lspruntime.Location, error)
	Hover(ctx context.Context, file string, line, col int) (lspruntime.HoverResult, error)
	Completion(ctx context.Context, file string, line, col, max int) ([]lspruntime.CompletionItem, error)
	DocumentSymbols(ctx context.Context, file string) ([]lspruntime.DocumentSymbol, error)
	Rename(ctx context.Context, file string, line, col int, newName string) (lspruntime.WorkspaceEdit, error)
	CodeActions(ctx context.Context, file string, line, col, endLine, endCol int) ([]lspruntime.CodeAction, error)
}

// Register adds the 7 LSP-backed tools when manager is non-nil.
func Register(reg *tool.Registry, manager Manager) error {
	if reg == nil {
		return fmt.Errorf("lsp tools: nil registry")
	}
	if manager == nil {
		return nil
	}
	for _, t := range []tool.Tool{
		newDiagnosticsTool(manager),
		newReferencesTool(manager),
		newHoverTool(manager),
		newCompletionTool(manager),
		newDocumentSymbolsTool(manager),
		newRenameTool(manager),
		newCodeActionTool(manager),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type diagnosticsArgs struct {
	File string `json:"file,omitempty" jsonschema:"description=Optional workspace file path. When omitted, returns cached diagnostics for all known files."`
}

type diagnosticsTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newDiagnosticsTool(manager Manager) *diagnosticsTool {
	return &diagnosticsTool{manager: manager, schema: jsonschema.Reflect(&diagnosticsArgs{})}
}

func (t *diagnosticsTool) Name() string { return "diagnostics" }
func (t *diagnosticsTool) Description() string {
	return "Read LSP diagnostics for one file or all known files."
}

func (t *diagnosticsTool) Schema() *jsonschema.Schema {
	return t.schema
}
func (t *diagnosticsTool) Risk() tool.Risk { return tool.RiskSafe }

func (t *diagnosticsTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args diagnosticsArgs
	if len(raw) > 0 {
		if err := tool.DecodeArgs("diagnostics", raw, &args); err != nil {
			return tool.Result{}, err
		}
	}
	ctx, cancel := lspToolContext(ctx)
	defer cancel()
	files, err := t.manager.Diagnostics(ctx, args.File)
	if err != nil {
		return tool.Result{}, err
	}
	text := formatDiagnostics(files)
	if text == "" {
		text = "no diagnostics"
	}
	return tool.Result{Content: text}, nil
}

type referencesArgs struct {
	Symbol string      `json:"symbol,omitempty" jsonschema:"description=Symbol name to find references for. Prefer this over line/col when available."`
	Path   string      `json:"path,omitempty"   jsonschema:"description=Optional file or directory to search for symbol before querying LSP. Defaults to the workspace root."`
	File   string      `json:"file,omitempty"   jsonschema:"description=Workspace file path. Required with line and col for position-based lookup; also accepted as symbol search scope."`
	Line   tool.IntArg `json:"line,omitempty"   jsonschema:"description=1-based line number for position-based lookup."`
	Col    tool.IntArg `json:"col,omitempty"    jsonschema:"description=1-based column number for position-based lookup."`
}

type referencesTool struct {
	manager Manager
	schema  *jsonschema.Schema
}

func newReferencesTool(manager Manager) *referencesTool {
	return &referencesTool{manager: manager, schema: jsonschema.Reflect(&referencesArgs{})}
}

func (t *referencesTool) Name() string { return "references" }
func (t *referencesTool) Description() string {
	return "Find LSP references for a symbol name, or for the symbol at a 1-based file position."
}

func (t *referencesTool) Schema() *jsonschema.Schema {
	return t.schema
}
func (t *referencesTool) Risk() tool.Risk { return tool.RiskSafe }

func (t *referencesTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args referencesArgs
	if err := tool.DecodeArgs("references", raw, &args); err != nil {
		return tool.Result{}, err
	}
	var locations []lspruntime.Location
	var err error
	if strings.TrimSpace(args.Symbol) != "" {
		ctx, cancel := lspToolContext(ctx)
		defer cancel()
		locations, err = t.manager.ReferencesBySymbol(ctx, args.Symbol, args.symbolSearchPath())
	} else {
		if strings.TrimSpace(args.File) == "" || int(args.Line) <= 0 || int(args.Col) <= 0 {
			return tool.Result{}, fmt.Errorf("references: provide symbol, or file with positive 1-based line and col")
		}
		ctx, cancel := lspToolContext(ctx)
		defer cancel()
		locations, err = t.manager.References(ctx, args.File, int(args.Line), int(args.Col))
	}
	if err != nil {
		return tool.Result{}, err
	}
	text := formatLocations(locations)
	if text == "" {
		text = "no references"
	}
	return tool.Result{Content: text}, nil
}

func (a referencesArgs) symbolSearchPath() string {
	if strings.TrimSpace(a.Path) != "" {
		return a.Path
	}
	return a.File
}

func formatDiagnostics(files []lspruntime.FileDiagnostics) string {
	var b strings.Builder
	for _, file := range files {
		for _, diag := range file.Diagnostics {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(
				&b, "%s:%d:%d: %s: %s",
				displayPath(file.Path),
				diag.Range.Start.Line+1,
				diag.Range.Start.Character+1,
				severityName(diag.Severity),
				diag.Message,
			)
			if diag.Source != "" {
				fmt.Fprintf(&b, " [%s]", diag.Source)
			}
		}
	}
	return b.String()
}

func formatLocations(locations []lspruntime.Location) string {
	var b strings.Builder
	for _, loc := range locations {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(
			&b, "%s:%d:%d",
			displayPath(pathFromURI(loc.URI)),
			loc.Range.Start.Line+1,
			loc.Range.Start.Character+1,
		)
	}
	return b.String()
}

func severityName(severity int) string {
	switch severity {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "diagnostic"
	}
}

func displayPath(path string) string {
	if path == "" {
		return "<unknown>"
	}
	if rel, err := filepath.Rel(".", path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func pathFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}
