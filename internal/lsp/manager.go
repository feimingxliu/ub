package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/config"
)

// Manager owns configured LSP clients and routes file sync operations.
// It is created by LazyManager.Start on first use and holds the running
// language server processes for the workspace.
type Manager struct {
	root    string
	servers []*server
}

// LazyManager defers starting language servers until an LSP query needs them.
// Filesystem change notifications are ignored until startup has happened; the
// first query syncs its target file before asking the server. This avoids
// the cost of spawning gopls/rust-analyzer/etc. for sessions that never use
// LSP tools.
type LazyManager struct {
	root    string
	configs map[string]config.LSPServerConfig

	mu       sync.Mutex
	started  bool
	manager  *Manager
	startErr error
}

// server holds one running language server client and the file types it
// handles. File types are matched against file extensions to route queries
type server struct {
	name      string
	client    *Client
	fileTypes []string
}

const (
	maxSymbolSearchBytes      = 2 * 1024 * 1024
	maxSymbolSearchCandidates = 100
)

var errStopSymbolSearch = errors.New("stop symbol search")

// NewLazyManager returns a Manager-compatible wrapper that does not spawn LSP
// processes until the first query. Nil is returned when no servers are
// configured so callers can keep the old "no LSP tools" behavior.
func NewLazyManager(root string, configs map[string]config.LSPServerConfig) *LazyManager {
	if len(configs) == 0 {
		return nil
	}
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &LazyManager{
		root:    root,
		configs: cloneServerConfigs(configs),
	}
}

// StartConfigured starts all configured LSP servers. Individual failures are
// returned as warnings so callers can keep non-LSP tools available.
func StartConfigured(ctx context.Context, root string, configs map[string]config.LSPServerConfig) (*Manager, []error) {
	if len(configs) == 0 {
		return nil, nil
	}
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, []error{err}
		}
		root = cwd
	}
	root, _ = filepath.Abs(root)
	m := &Manager{root: root}
	var warnings []error
	for _, name := range sortedConfigNames(configs) {
		cfg := configs[name]
		client, err := Start(ctx, ServerConfig{
			Command:   cfg.Command,
			Args:      cfg.Args,
			Root:      root,
			FileTypes: cfg.FileTypes,
		})
		if err != nil {
			warnings = append(warnings, fmt.Errorf("lsp server %q: %w", name, err))
			continue
		}
		m.servers = append(m.servers, &server{name: name, client: client, fileTypes: normalizeFileTypes(cfg.FileTypes)})
	}
	if len(m.servers) == 0 {
		return nil, warnings
	}
	return m, warnings
}

// ensureStarted lazily starts all configured LSP servers on first use.
// It is idempotent and thread-safe: subsequent calls return the cached
// Manager or the cached error. This defers the cost of spawning language
// servers until an LSP tool is actually called.
func (m *LazyManager) ensureStarted(ctx context.Context) (*Manager, error) {
	if m == nil {
		return nil, fmt.Errorf("lsp: no language server configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		if m.manager != nil {
			return m.manager, nil
		}
		return nil, m.startErr
	}
	m.started = true
	manager, warnings := StartConfigured(ctx, m.root, m.configs)
	m.manager = manager
	if manager != nil {
		return manager, nil
	}
	if len(warnings) > 0 {
		m.startErr = fmt.Errorf("lsp: start configured: %w", errors.Join(warnings...))
	} else {
		m.startErr = fmt.Errorf("lsp: no language server configured")
	}
	return nil, m.startErr
}

// withLazyManager runs fn against the underlying Manager, automatically
// restarting the LSP servers once if the first call fails with
// ErrServerUnavailable (indicating the server process died). This provides
// transparent recovery from crashed language servers without surfacing the
// error to the caller on the first attempt.
func withLazyManager[T any](lm *LazyManager, ctx context.Context, fn func(*Manager) (T, error)) (T, error) {
	var zero T
	manager, err := lm.ensureStarted(ctx)
	if err != nil {
		return zero, err
	}
	out, err := fn(manager)
	if !errors.Is(err, ErrServerUnavailable) {
		return out, err
	}
	lm.resetManager(manager)
	manager, startErr := lm.ensureStarted(ctx)
	if startErr != nil {
		return zero, startErr
	}
	out, err = fn(manager)
	if errors.Is(err, ErrServerUnavailable) {
		lm.resetManager(manager)
	}
	return out, err
}

// resetManager tears down a dead Manager so the next ensureStarted call
// spawns fresh language server processes. The old Manager is closed to
// release its stdio pipes.
func (m *LazyManager) resetManager(manager *Manager) {
	if m == nil || manager == nil {
		return
	}
	m.mu.Lock()
	if m.manager == manager {
		m.manager = nil
		m.started = false
		m.startErr = nil
	}
	m.mu.Unlock()
	_ = manager.Close()
}

// DidChangeFile updates an already-started server. Before the first query it is
// a no-op so ordinary file edits do not pay LSP startup cost.
func (m *LazyManager) DidChangeFile(ctx context.Context, path string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	manager := m.manager
	m.mu.Unlock()
	if manager == nil {
		return nil
	}
	if err := manager.DidChangeFile(ctx, path); err != nil {
		if errors.Is(err, ErrServerUnavailable) {
			m.resetManager(manager)
			return nil
		}
		return err
	}
	return nil
}

func (m *LazyManager) Diagnostics(ctx context.Context, file string) ([]FileDiagnostics, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]FileDiagnostics, error) {
		return manager.Diagnostics(ctx, file)
	})
}

func (m *LazyManager) References(ctx context.Context, file string, line, col int) ([]Location, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]Location, error) {
		return manager.References(ctx, file, line, col)
	})
}

func (m *LazyManager) ReferencesBySymbol(ctx context.Context, symbol, path string) ([]Location, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]Location, error) {
		return manager.ReferencesBySymbol(ctx, symbol, path)
	})
}

func (m *LazyManager) Hover(ctx context.Context, file string, line, col int) (HoverResult, error) {
	return withLazyManager(m, ctx, func(manager *Manager) (HoverResult, error) {
		return manager.Hover(ctx, file, line, col)
	})
}

func (m *LazyManager) Completion(ctx context.Context, file string, line, col, max int) ([]CompletionItem, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]CompletionItem, error) {
		return manager.Completion(ctx, file, line, col, max)
	})
}

func (m *LazyManager) DocumentSymbols(ctx context.Context, file string) ([]DocumentSymbol, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]DocumentSymbol, error) {
		return manager.DocumentSymbols(ctx, file)
	})
}

func (m *LazyManager) Rename(ctx context.Context, file string, line, col int, newName string) (WorkspaceEdit, error) {
	return withLazyManager(m, ctx, func(manager *Manager) (WorkspaceEdit, error) {
		return manager.Rename(ctx, file, line, col, newName)
	})
}

func (m *LazyManager) CodeActions(ctx context.Context, file string, line, col, endLine, endCol int) ([]CodeAction, error) {
	return withLazyManager(m, ctx, func(manager *Manager) ([]CodeAction, error) {
		return manager.CodeActions(ctx, file, line, col, endLine, endCol)
	})
}

func (m *LazyManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	manager := m.manager
	m.manager = nil
	m.mu.Unlock()
	if manager == nil {
		return nil
	}
	return manager.Close()
}

// DidChangeFile reads path and syncs it to the matching language server.
func (m *Manager) DidChangeFile(ctx context.Context, path string) error {
	if m == nil {
		return nil
	}
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return err
	}
	srv := m.serverFor(abs)
	if srv == nil {
		return nil
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("lsp: read changed file %s: %w", abs, err)
	}
	return srv.client.DidChange(ctx, abs, string(content))
}

// Diagnostics returns cached diagnostics, syncing a target file first when
// path is not empty.
func (m *Manager) Diagnostics(ctx context.Context, path string) ([]FileDiagnostics, error) {
	if m == nil {
		return nil, fmt.Errorf("lsp: no language server configured")
	}
	if strings.TrimSpace(path) == "" {
		var out []FileDiagnostics
		for _, srv := range m.servers {
			out = append(out, srv.client.allDiagnostics()...)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
		return out, nil
	}
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return nil, err
	}
	srv := m.serverFor(abs)
	if srv == nil {
		return nil, fmt.Errorf("lsp: no language server for %s", abs)
	}
	if err := m.DidChangeFile(ctx, abs); err != nil {
		return nil, err
	}
	uri, err := fileURI(abs)
	if err != nil {
		return nil, err
	}
	diags, _ := waitDiagnostics(ctx, srv.client, uri, 250*time.Millisecond)
	return []FileDiagnostics{{
		URI:         uri,
		Path:        abs,
		Diagnostics: diags,
	}}, nil
}

// References queries textDocument/references at a one-based file position.
func (m *Manager) References(ctx context.Context, path string, line, col int) ([]Location, error) {
	if m == nil {
		return nil, fmt.Errorf("lsp: no language server configured")
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("lsp: file is required")
	}
	if line <= 0 || col <= 0 {
		return nil, fmt.Errorf("lsp: line and col must be positive 1-based values")
	}
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return nil, err
	}
	srv := m.serverFor(abs)
	if srv == nil {
		return nil, fmt.Errorf("lsp: no language server for %s", abs)
	}
	if err := m.DidChangeFile(ctx, abs); err != nil {
		return nil, err
	}
	uri, err := fileURI(abs)
	if err != nil {
		return nil, err
	}
	var out []Location
	err = srv.client.Call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position": map[string]any{
			"line":      line - 1,
			"character": col - 1,
		},
		"context": map[string]any{"includeDeclaration": true},
	}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReferencesBySymbol searches for a symbol in workspace files and queries LSP
// references at the first matching identifier positions.
func (m *Manager) ReferencesBySymbol(ctx context.Context, symbol, searchPath string) ([]Location, error) {
	if m == nil {
		return nil, fmt.Errorf("lsp: no language server configured")
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("lsp: symbol is required")
	}
	candidates, err := m.symbolCandidates(ctx, symbol, searchPath)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	var errs []error
	for _, candidate := range candidates {
		locations, err := m.References(ctx, candidate.path, candidate.line, candidate.col)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(locations) > 0 {
			return locations, nil
		}
	}
	return nil, errors.Join(errs...)
}

// prepPosition resolves path to absolute, picks the right server, ensures
// the document has been opened/refreshed, and returns the textDocument URI
// plus the resolved server. It exists so the five position-based LSP
// methods below share the same error-handling boilerplate.
func (m *Manager) prepPosition(ctx context.Context, path string, line, col int, allowEnd bool) (*server, string, error) {
	if m == nil {
		return nil, "", fmt.Errorf("lsp: no language server configured")
	}
	if strings.TrimSpace(path) == "" {
		return nil, "", fmt.Errorf("lsp: file is required")
	}
	if line <= 0 || col <= 0 {
		return nil, "", fmt.Errorf("lsp: line and col must be positive 1-based values")
	}
	_ = allowEnd
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return nil, "", err
	}
	srv := m.serverFor(abs)
	if srv == nil {
		return nil, "", fmt.Errorf("lsp: no language server for %s", abs)
	}
	if err := m.DidChangeFile(ctx, abs); err != nil {
		return nil, "", err
	}
	uri, err := fileURI(abs)
	if err != nil {
		return nil, "", err
	}
	return srv, uri, nil
}

// Hover queries textDocument/hover at a 1-based position.
func (m *Manager) Hover(ctx context.Context, path string, line, col int) (HoverResult, error) {
	srv, uri, err := m.prepPosition(ctx, path, line, col, false)
	if err != nil {
		return HoverResult{}, err
	}
	var raw json.RawMessage
	err = srv.client.Call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line - 1, "character": col - 1},
	}, &raw)
	if err != nil {
		return HoverResult{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return HoverResult{}, nil
	}
	var hover struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range,omitempty"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return HoverResult{}, fmt.Errorf("lsp: decode hover: %w", err)
	}
	text, err := flattenHoverContents(hover.Contents)
	if err != nil {
		return HoverResult{}, err
	}
	return HoverResult{Contents: text, Range: hover.Range}, nil
}

// Completion queries textDocument/completion at a 1-based position. The
// response is normalized whether the server returns CompletionList or a
// bare CompletionItem array. max <= 0 means "no truncation".
func (m *Manager) Completion(ctx context.Context, path string, line, col, maxItems int) ([]CompletionItem, error) {
	srv, uri, err := m.prepPosition(ctx, path, line, col, false)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = srv.client.Call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line - 1, "character": col - 1},
	}, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	items, err := parseCompletion(raw)
	if err != nil {
		return nil, err
	}
	if maxItems > 0 && len(items) > maxItems {
		items = items[:maxItems]
	}
	return items, nil
}

// DocumentSymbols queries textDocument/documentSymbol and returns the
// hierarchical form. If the server only supports flat SymbolInformation,
// this method still returns DocumentSymbol shells with empty Children.
func (m *Manager) DocumentSymbols(ctx context.Context, path string) ([]DocumentSymbol, error) {
	if m == nil {
		return nil, fmt.Errorf("lsp: no language server configured")
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("lsp: file is required")
	}
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return nil, err
	}
	srv := m.serverFor(abs)
	if srv == nil {
		return nil, fmt.Errorf("lsp: no language server for %s", abs)
	}
	if err := m.DidChangeFile(ctx, abs); err != nil {
		return nil, err
	}
	uri, err := fileURI(abs)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = srv.client.Call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return parseDocumentSymbols(raw)
}

// Rename queries textDocument/rename and returns the proposed edits
// normalized as []TextEdit (one entry per concrete edit). It does NOT apply
// the edits; callers (typically the rename tool) format them for the agent
// to apply via apply_patch or multiedit.
func (m *Manager) Rename(ctx context.Context, path string, line, col int, newName string) (WorkspaceEdit, error) {
	if strings.TrimSpace(newName) == "" {
		return WorkspaceEdit{}, fmt.Errorf("lsp: new_name is required")
	}
	srv, uri, err := m.prepPosition(ctx, path, line, col, false)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	var raw json.RawMessage
	err = srv.client.Call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line - 1, "character": col - 1},
		"newName":      newName,
	}, &raw)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return WorkspaceEdit{}, nil
	}
	return parseWorkspaceEdit(raw)
}

// CodeActions queries textDocument/codeAction over a range. endLine/endCol
// = 0 mean "use start as a point range." It always returns the normalized
// [{Title, Kind, HasEdit}] regardless of whether the server returns the
// older Command shape or the newer CodeAction shape.
func (m *Manager) CodeActions(ctx context.Context, path string, line, col, endLine, endCol int) ([]CodeAction, error) {
	srv, uri, err := m.prepPosition(ctx, path, line, col, true)
	if err != nil {
		return nil, err
	}
	if endLine <= 0 {
		endLine = line
	}
	if endCol <= 0 {
		endCol = col
	}
	var raw json.RawMessage
	err = srv.client.Call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": line - 1, "character": col - 1},
			"end":   map[string]any{"line": endLine - 1, "character": endCol - 1},
		},
		"context": map[string]any{"diagnostics": []any{}},
	}, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return parseCodeActions(raw)
}

// Close closes all managed LSP clients.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var err error
	for _, srv := range m.servers {
		if closeErr := srv.client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

type symbolCandidate struct {
	path string
	line int
	col  int
}

func (m *Manager) symbolCandidates(ctx context.Context, symbol, searchPath string) ([]symbolCandidate, error) {
	base, explicitFile, err := m.resolveSymbolSearchPath(searchPath)
	if err != nil {
		return nil, err
	}
	candidates := make([]symbolCandidate, 0, 8)
	add := func(path string, strict bool) error {
		if len(candidates) >= maxSymbolSearchCandidates {
			return errStopSymbolSearch
		}
		matches, err := m.fileSymbolCandidates(path, symbol)
		if err != nil {
			if strict {
				return err
			}
			return nil
		}
		candidates = append(candidates, matches...)
		if len(candidates) >= maxSymbolSearchCandidates {
			candidates = candidates[:maxSymbolSearchCandidates]
			return errStopSymbolSearch
		}
		return nil
	}
	if explicitFile {
		if err := add(base, true); err != nil && !errors.Is(err, errStopSymbolSearch) {
			return nil, err
		}
		return candidates, nil
	}
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			if shouldSkipSymbolSearchDir(path, base, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return add(path, false)
	})
	if errors.Is(err, errStopSymbolSearch) {
		return candidates, nil
	}
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func (m *Manager) resolveSymbolSearchPath(searchPath string) (string, bool, error) {
	base := strings.TrimSpace(searchPath)
	if base == "" {
		base = m.root
	} else if !filepath.IsAbs(base) {
		base = filepath.Join(m.root, base)
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", false, err
	}
	if err := ensureInsideRoot(m.root, base); err != nil {
		return "", false, err
	}
	info, err := os.Stat(base)
	if err != nil {
		return "", false, fmt.Errorf("lsp: symbol search path %s: %w", base, err)
	}
	return base, !info.IsDir(), nil
}

func (m *Manager) fileSymbolCandidates(path, symbol string) ([]symbolCandidate, error) {
	abs, err := m.resolveWorkspaceFile(path)
	if err != nil {
		return nil, err
	}
	if m.serverFor(abs) == nil {
		return nil, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSymbolSearchBytes {
		return nil, nil
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return findSymbolCandidates(abs, string(content), symbol), nil
}

func findSymbolCandidates(path, content, symbol string) []symbolCandidate {
	var candidates []symbolCandidate
	lines := strings.Split(content, "\n")
	queries := []struct {
		text      string
		colOffset int
	}{
		{text: symbol, colOffset: symbolColumnOffset(symbol)},
	}
	base := symbolBase(symbol)
	if base != symbol {
		queries = append(queries, struct {
			text      string
			colOffset int
		}{text: base})
	}
	for _, query := range queries {
		if query.text == "" {
			continue
		}
		for lineIdx, line := range lines {
			start := 0
			for {
				pos := strings.Index(line[start:], query.text)
				if pos < 0 {
					break
				}
				pos += start
				if symbolMatchBoundary(line, pos, pos+len(query.text)) {
					candidates = append(candidates, symbolCandidate{
						path: path,
						line: lineIdx + 1,
						col:  pos + query.colOffset + 1,
					})
					if len(candidates) >= maxSymbolSearchCandidates {
						return candidates
					}
				}
				start = pos + len(query.text)
			}
		}
		if len(candidates) > 0 {
			return candidates
		}
	}
	return candidates
}

func symbolColumnOffset(symbol string) int {
	return len(symbol) - len(symbolBase(symbol))
}

func symbolBase(symbol string) string {
	offset := 0
	if idx := strings.LastIndex(symbol, "::"); idx >= 0 && idx+2 > offset {
		offset = idx + 2
	}
	if idx := strings.LastIndex(symbol, "."); idx >= 0 && idx+1 > offset {
		offset = idx + 1
	}
	if idx := strings.LastIndex(symbol, "#"); idx >= 0 && idx+1 > offset {
		offset = idx + 1
	}
	return symbol[offset:]
}

func symbolMatchBoundary(line string, start, end int) bool {
	if start > 0 && isIdentifierByte(line[start-1]) {
		return false
	}
	if end < len(line) && isIdentifierByte(line[end]) {
		return false
	}
	return true
}

func isIdentifierByte(b byte) bool {
	return b == '_' ||
		('0' <= b && b <= '9') ||
		('a' <= b && b <= 'z') ||
		('A' <= b && b <= 'Z')
}

func shouldSkipSymbolSearchDir(path, root, name string) bool {
	if path == root {
		return false
	}
	switch name {
	case ".git", ".hg", ".svn":
		return true
	default:
		return false
	}
}

func ensureInsideRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("lsp: path %s is outside workspace root %s", path, root)
	}
	return nil
}

func waitDiagnostics(ctx context.Context, c *Client, uri string, timeout time.Duration) ([]Diagnostic, bool) {
	if diags, ok := c.diagnosticsFor(uri); ok {
		return diags, true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if diags, ok := c.diagnosticsFor(uri); ok {
				return diags, true
			}
		case <-timer.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
}

func (m *Manager) serverFor(path string) *server {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	for _, srv := range m.servers {
		if len(srv.fileTypes) == 0 {
			return srv
		}
		for _, typ := range srv.fileTypes {
			if typ == ext || typ == "."+ext {
				return srv
			}
		}
	}
	return nil
}

func (m *Manager) resolveWorkspaceFile(path string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("lsp: no language server configured")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("lsp: file is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := ensureInsideRoot(m.root, abs); err != nil {
		return "", err
	}
	return abs, nil
}

func sortedConfigNames(configs map[string]config.LSPServerConfig) []string {
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeFileTypes(types []string) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, typ := range types {
		typ = strings.TrimSpace(strings.ToLower(typ))
		typ = strings.TrimPrefix(typ, ".")
		if typ != "" {
			out = append(out, typ)
		}
	}
	return out
}

func cloneServerConfigs(configs map[string]config.LSPServerConfig) map[string]config.LSPServerConfig {
	if len(configs) == 0 {
		return nil
	}
	out := make(map[string]config.LSPServerConfig, len(configs))
	for name, cfg := range configs {
		cfg.Args = append([]string(nil), cfg.Args...)
		cfg.FileTypes = append([]string(nil), cfg.FileTypes...)
		out[name] = cfg
	}
	return out
}
