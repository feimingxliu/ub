// Package mcp adapts remote MCP tools to ub's local tool interface.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/config"
	coremcp "github.com/feimingxliu/ub/internal/mcp"
	"github.com/feimingxliu/ub/internal/tool"
)

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// Warning describes one non-fatal MCP startup or registration failure.
type Warning struct {
	Server string
	Err    error
}

func (w Warning) Error() string {
	if w.Server == "" {
		return w.Err.Error()
	}
	return fmt.Sprintf("mcp server %q: %v", w.Server, w.Err)
}

// RegisterConfigured starts configured MCP servers and registers their tools.
// Individual server failures are returned as warnings and do not stop other
// servers from being registered.
func RegisterConfigured(ctx context.Context, reg *tool.Registry, servers map[string]config.MCPServerConfig) (func() error, []error) {
	if len(servers) == 0 {
		return func() error { return nil }, nil
	}
	var clients []*coremcp.Client
	var warnings []error
	for _, name := range sortedServerNames(servers) {
		cfg := servers[name]
		client, err := clientForConfig(ctx, cfg)
		if err != nil {
			warnings = append(warnings, Warning{Server: name, Err: err})
			continue
		}
		if _, err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			warnings = append(warnings, Warning{Server: name, Err: err})
			continue
		}
		specs, err := client.ListTools(ctx)
		if err != nil {
			_ = client.Close()
			warnings = append(warnings, Warning{Server: name, Err: err})
			continue
		}
		registered := false
		for _, spec := range specs {
			t := NewTool(name, client, spec)
			if err := reg.Register(t); err != nil {
				warnings = append(warnings, Warning{Server: name, Err: err})
				continue
			}
			registered = true
		}
		if registered {
			clients = append(clients, client)
		} else {
			_ = client.Close()
		}
	}
	return func() error {
		var err error
		for _, client := range clients {
			if closeErr := client.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		return err
	}, warnings
}

func clientForConfig(ctx context.Context, cfg config.MCPServerConfig) (*coremcp.Client, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "", "stdio":
		return coremcp.NewStdioClient(ctx, coremcp.StdioConfig{
			Command: cfg.Command,
			Args:    cfg.Args,
			Env:     cfg.Env,
		})
	case "http":
		return coremcp.NewHTTPClient(coremcp.HTTPConfig{URL: cfg.URL, Headers: cfg.Headers})
	case "sse":
		return coremcp.NewSSEClient(ctx, coremcp.SSEConfig{URL: cfg.URL, Headers: cfg.Headers})
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", cfg.Type)
	}
}

func sortedServerNames(servers map[string]config.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Tool is a local tool backed by one remote MCP tool.
type Tool struct {
	serverName string
	remoteName string
	name       string
	desc       string
	schema     *jsonschema.Schema
	client     *coremcp.Client
}

// NewTool wraps one remote MCP tool definition.
func NewTool(serverName string, client *coremcp.Client, spec coremcp.ToolSpec) *Tool {
	return &Tool{
		serverName: serverName,
		remoteName: spec.Name,
		name:       "mcp__" + safeName(serverName) + "__" + safeName(spec.Name),
		desc:       spec.Description,
		schema:     schemaFromRaw(spec.InputSchema),
		client:     client,
	}
}

func (t *Tool) Name() string { return t.name }
func (t *Tool) Description() string {
	if strings.TrimSpace(t.desc) == "" {
		return fmt.Sprintf("MCP tool %s from server %s.", t.remoteName, t.serverName)
	}
	return t.desc
}
func (t *Tool) Schema() *jsonschema.Schema { return t.schema }
func (t *Tool) Risk() tool.Risk            { return tool.RiskExec }

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	res, err := t.client.CallTool(ctx, t.remoteName, args)
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Content: res.Text(), IsError: res.IsError}, nil
}

func safeName(name string) string {
	name = strings.Trim(unsafeNameChars.ReplaceAllString(strings.TrimSpace(name), "_"), "_")
	if name == "" {
		return "unnamed"
	}
	return name
}

func schemaFromRaw(raw json.RawMessage) *jsonschema.Schema {
	if len(raw) == 0 {
		return jsonschema.Reflect(&map[string]any{})
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return jsonschema.Reflect(&map[string]any{})
	}
	return &schema
}
