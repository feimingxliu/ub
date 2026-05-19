package lsp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/config"
)

// Manager owns configured LSP clients and routes file sync operations.
type Manager struct {
	root    string
	servers []*server
}

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

// DidChangeFile reads path and syncs it to the matching language server.
func (m *Manager) DidChangeFile(ctx context.Context, path string) error {
	if m == nil {
		return nil
	}
	abs, err := filepath.Abs(path)
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
	abs, err := filepath.Abs(path)
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
	abs, err := filepath.Abs(path)
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
	abs, err := filepath.Abs(path)
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
