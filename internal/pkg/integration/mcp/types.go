// Package mcp implements ub's Model Context Protocol client.
package mcp

import (
	"encoding/json"
	"strings"
)

const protocolVersion = "2024-11-05"

// StdioConfig configures a stdio MCP server process.
type StdioConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

// HTTPConfig configures a JSON-RPC-over-HTTP MCP server.
type HTTPConfig struct {
	URL     string
	Headers map[string]string
}

// SSEConfig configures an MCP SSE endpoint.
type SSEConfig struct {
	URL     string
	Headers map[string]string
}

// ToolSpec is the subset of an MCP tool definition ub needs to expose tools.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ContentBlock is one item from an MCP tools/call result.
type ContentBlock struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// CallResult is the result returned by MCP tools/call.
type CallResult struct {
	Content []ContentBlock `json:"content,omitempty"`
	IsError bool           `json:"isError,omitempty"`
}

// Text joins text content blocks for display in ub tool results.
func (r CallResult) Text() string {
	var parts []string
	for _, block := range r.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
			continue
		}
		if len(block.Data) > 0 {
			parts = append(parts, string(block.Data))
		}
	}
	return strings.Join(parts, "\n")
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      implementation `json:"clientInfo"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is returned by an MCP server after initialize.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ServerInfo      implementation `json:"serverInfo,omitempty"`
}

type listToolsResult struct {
	Tools []ToolSpec `json:"tools"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
