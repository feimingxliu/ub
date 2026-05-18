package lsp

import (
	"context"
	"fmt"
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
