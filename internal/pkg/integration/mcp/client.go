package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
)

type transport interface {
	Call(ctx context.Context, req rpcRequest) (json.RawMessage, error)
	Notify(ctx context.Context, method string, params any) error
	Close() error
}

// Client speaks the MCP request/response methods ub needs.
type Client struct {
	tr     transport
	nextID atomic.Int64
}

func newClient(tr transport) *Client {
	c := &Client{tr: tr}
	c.nextID.Store(1)
	return c
}

// NewStdioClient starts a stdio MCP server and returns a client bound to it.
func NewStdioClient(ctx context.Context, cfg StdioConfig) (*Client, error) {
	tr, err := newStdioTransport(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newClient(tr), nil
}

// NewHTTPClient returns a client that sends JSON-RPC requests over HTTP POST.
func NewHTTPClient(cfg HTTPConfig) (*Client, error) {
	tr, err := newHTTPTransport(cfg)
	if err != nil {
		return nil, err
	}
	return newClient(tr), nil
}

// NewSSEClient opens an SSE stream and returns a client that posts requests
// to the endpoint announced by the stream.
func NewSSEClient(ctx context.Context, cfg SSEConfig) (*Client, error) {
	tr, err := newSSETransport(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newClient(tr), nil
}

// Initialize performs the MCP initialize handshake.
func (c *Client) Initialize(ctx context.Context) (InitializeResult, error) {
	var out InitializeResult
	err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo: implementation{
			Name:    "ub",
			Version: "dev",
		},
	}, &out)
	if err != nil {
		return InitializeResult{}, err
	}
	if err := c.tr.Notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return InitializeResult{}, err
	}
	return out, nil
}

// ListTools calls the MCP tools/list method.
func (c *Client) ListTools(ctx context.Context) ([]ToolSpec, error) {
	var out listToolsResult
	if err := c.call(ctx, "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool calls one MCP tool with raw JSON arguments.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	if name == "" {
		return CallResult{}, errors.New("mcp: tool name is required")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var out CallResult
	if err := c.call(ctx, "tools/call", callToolParams{Name: name, Arguments: args}, &out); err != nil {
		return CallResult{}, err
	}
	return out, nil
}

// Close releases the underlying transport.
func (c *Client) Close() error {
	if c == nil || c.tr == nil {
		return nil
	}
	return c.tr.Close()
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	result, err := c.tr.Call(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if len(result) == 0 {
		return fmt.Errorf("mcp: %s returned empty result", method)
	}
	if err := json.Unmarshal(result, out); err != nil {
		return fmt.Errorf("mcp: decode %s result: %w", method, err)
	}
	return nil
}
