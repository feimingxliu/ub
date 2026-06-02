// Package mcp adapts remote MCP tools to ub's local tool interface.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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

// ServerStatus reports a point-in-time MCP connectivity check.
type ServerStatus struct {
	Name      string
	Type      string
	Status    string
	ToolCount int
	Err       error
}

// Status reports the current state of this connection: "connected",
// "disconnected", or "backoff" (reconnect pending).
func (c *serverConnection) Status() ServerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := ServerStatus{Name: c.name, Type: serverType(c.cfg)}
	if c.client != nil {
		st.Status = "connected"
		return st
	}
	if c.lastErr != nil {
		st.Err = c.lastErr
		if !c.nextAttempt.IsZero() && time.Now().Before(c.nextAttempt) {
			st.Status = "backoff"
		} else {
			st.Status = "disconnected"
		}
		return st
	}
	st.Status = "disconnected"
	return st
}

// CheckConfigured probes configured MCP servers without registering tools.
func CheckConfigured(ctx context.Context, servers map[string]config.MCPServerConfig) []ServerStatus {
	if len(servers) == 0 {
		return nil
	}
	statuses := make([]ServerStatus, 0, len(servers))
	for _, name := range sortedServerNames(servers) {
		cfg := servers[name]
		status := ServerStatus{Name: name, Type: strings.TrimSpace(cfg.Type)}
		if status.Type == "" {
			status.Type = "stdio"
		}
		client, specs, err := connectMCPServer(ctx, cfg)
		if err != nil {
			status.Status = "error"
			status.Err = err
			statuses = append(statuses, status)
			continue
		}
		_ = client.Close()
		status.ToolCount = len(specs)
		if len(specs) == 0 {
			status.Status = "no_tools"
		} else {
			status.Status = "connected"
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// Connections holds the live MCP server connections created by
// RegisterConfigured. It supports querying the runtime state of each
// connection so that ub doctor can report whether a server is connected,
// disconnected, or in reconnect backoff without opening a fresh connection.
type Connections struct {
	mu      sync.Mutex
	entries []connectionStatus
}

type connectionStatus struct {
	conn   *serverConnection
	static ServerStatus
}

// Status returns the current status of every live connection.
func (cs *Connections) Status() []ServerStatus {
	cs.mu.Lock()
	entries := append([]connectionStatus(nil), cs.entries...)
	cs.mu.Unlock()
	if len(entries) == 0 {
		return nil
	}
	out := make([]ServerStatus, len(entries))
	for i, entry := range entries {
		if entry.conn == nil {
			out[i] = entry.static
			continue
		}
		status := entry.conn.Status()
		status.ToolCount = entry.static.ToolCount
		out[i] = status
	}
	return out
}

// Close shuts down all connections.
func (cs *Connections) Close() error {
	cs.mu.Lock()
	entries := cs.entries
	cs.entries = nil
	cs.mu.Unlock()
	var err error
	for _, entry := range entries {
		if entry.conn == nil {
			continue
		}
		if closeErr := entry.conn.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// RegisterConfigured starts configured MCP servers and registers their tools.
// Individual server failures are returned as warnings and do not stop other
// servers from being registered. The returned Connections value supports
// live status queries (Connections.Status); call Connections.Close when
// shutting down.
func RegisterConfigured(ctx context.Context, reg *tool.Registry, servers map[string]config.MCPServerConfig) (*Connections, []error) {
	if len(servers) == 0 {
		return &Connections{}, nil
	}
	cs := &Connections{}
	var warnings []error
	for _, name := range sortedServerNames(servers) {
		cfg := servers[name]
		conn := newServerConnection(name, cfg, connectMCPServer)
		specs, err := conn.Connect(ctx)
		if err != nil {
			warnings = append(warnings, Warning{Server: name, Err: err})
			cs.entries = append(cs.entries, connectionStatus{static: ServerStatus{
				Name:   name,
				Type:   serverType(cfg),
				Status: "error",
				Err:    err,
			}})
			continue
		}
		registered := false
		toolCount := 0
		for _, spec := range specs {
			t := NewTool(name, conn, spec)
			if err := reg.Register(t); err != nil {
				warnings = append(warnings, Warning{Server: name, Err: err})
				continue
			}
			registered = true
			toolCount++
		}
		if registered {
			cs.entries = append(cs.entries, connectionStatus{
				conn: conn,
				static: ServerStatus{
					Name:      name,
					Type:      serverType(cfg),
					Status:    "connected",
					ToolCount: toolCount,
				},
			})
		} else {
			_ = conn.Close()
			cs.entries = append(cs.entries, connectionStatus{static: ServerStatus{
				Name:   name,
				Type:   serverType(cfg),
				Status: "no_tools",
			}})
		}
	}
	return cs, warnings
}

type connector func(context.Context, config.MCPServerConfig) (*coremcp.Client, []coremcp.ToolSpec, error)

func connectMCPServer(ctx context.Context, cfg config.MCPServerConfig) (*coremcp.Client, []coremcp.ToolSpec, error) {
	client, err := clientForConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	if _, err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	specs, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return client, specs, nil
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

func serverType(cfg config.MCPServerConfig) string {
	typ := strings.TrimSpace(cfg.Type)
	if typ == "" {
		return "stdio"
	}
	return typ
}

const (
	reconnectInitialBackoff = 500 * time.Millisecond
	reconnectMaxBackoff     = 30 * time.Second
)

type serverConnection struct {
	name    string
	cfg     config.MCPServerConfig
	connect connector

	mu          sync.Mutex
	client      *coremcp.Client
	backoff     time.Duration
	nextAttempt time.Time
	lastErr     error
}

func newServerConnection(name string, cfg config.MCPServerConfig, connect connector) *serverConnection {
	return &serverConnection{
		name:    name,
		cfg:     cfg,
		connect: connect,
		backoff: reconnectInitialBackoff,
	}
}

func (c *serverConnection) Connect(ctx context.Context) ([]coremcp.ToolSpec, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	specs, err := c.connectLocked(ctx, true)
	if err != nil {
		return nil, err
	}
	return specs, nil
}

func (c *serverConnection) CallTool(ctx context.Context, name string, args json.RawMessage) (coremcp.CallResult, error) {
	client, err := c.clientForCall(ctx)
	if err != nil {
		return coremcp.CallResult{}, err
	}
	res, err := client.CallTool(ctx, name, args)
	if err == nil {
		c.markHealthy()
		return res, nil
	}
	// Server-side JSON-RPC errors and caller-cancellation are not connection
	// faults: surface them as-is so a tool that fails with an application
	// error (or a cancelled context) does not get retransmitted. Only
	// transport failures (EOF, broken pipe, decode errors, HTTP non-2xx)
	// reconnect and replay the call.
	if coremcp.IsServerError(err) || ctx.Err() != nil {
		c.markHealthy()
		return coremcp.CallResult{}, err
	}
	c.markDisconnected(err)

	client, reconnectErr := c.reconnectNow(ctx)
	if reconnectErr != nil {
		return coremcp.CallResult{}, fmt.Errorf("%w; reconnect failed: %v", err, reconnectErr)
	}
	res, err = client.CallTool(ctx, name, args)
	if err != nil {
		if !coremcp.IsServerError(err) && ctx.Err() == nil {
			c.markDisconnected(err)
		}
		return coremcp.CallResult{}, err
	}
	c.markHealthy()
	return res, nil
}

func (c *serverConnection) Close() error {
	c.mu.Lock()
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (c *serverConnection) clientForCall(ctx context.Context) (*coremcp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client, nil
	}
	if !c.nextAttempt.IsZero() && time.Now().Before(c.nextAttempt) {
		return nil, fmt.Errorf("mcp server %q is disconnected; reconnect backoff active: %v", c.name, c.lastErr)
	}
	if _, err := c.connectLocked(ctx, false); err != nil {
		return nil, err
	}
	return c.client, nil
}

func (c *serverConnection) reconnectNow(ctx context.Context) (*coremcp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.connectLocked(ctx, true); err != nil {
		return nil, err
	}
	return c.client, nil
}

func (c *serverConnection) connectLocked(ctx context.Context, ignoreBackoff bool) ([]coremcp.ToolSpec, error) {
	if c.connect == nil {
		return nil, fmt.Errorf("mcp server %q has no connector", c.name)
	}
	if !ignoreBackoff && !c.nextAttempt.IsZero() && time.Now().Before(c.nextAttempt) {
		return nil, fmt.Errorf("mcp server %q reconnect backoff active: %v", c.name, c.lastErr)
	}
	client, specs, err := c.connect(ctx, c.cfg)
	if err != nil {
		c.recordConnectFailure(err)
		return nil, err
	}
	if c.client != nil {
		_ = c.client.Close()
	}
	c.client = client
	c.lastErr = nil
	c.nextAttempt = time.Time{}
	c.backoff = reconnectInitialBackoff
	return specs, nil
}

func (c *serverConnection) markHealthy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastErr = nil
	c.nextAttempt = time.Time{}
	c.backoff = reconnectInitialBackoff
}

func (c *serverConnection) markDisconnected(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
	c.recordConnectFailure(err)
}

func (c *serverConnection) recordConnectFailure(err error) {
	c.lastErr = err
	c.nextAttempt = time.Now().Add(c.backoff)
	c.backoff *= 2
	if c.backoff > reconnectMaxBackoff {
		c.backoff = reconnectMaxBackoff
	}
}

// Tool is a local tool backed by one remote MCP tool.
type Tool struct {
	serverName string
	remoteName string
	name       string
	desc       string
	schema     *jsonschema.Schema
	client     interface {
		CallTool(context.Context, string, json.RawMessage) (coremcp.CallResult, error)
	}
}

// NewTool wraps one remote MCP tool definition.
func NewTool(serverName string, client interface {
	CallTool(context.Context, string, json.RawMessage) (coremcp.CallResult, error)
}, spec coremcp.ToolSpec,
) *Tool {
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
