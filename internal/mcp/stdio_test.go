package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestStdioClientListsAndCallsTools(t *testing.T) {
	temp := t.TempDir()
	path := temp + "/hello.txt"
	if err := os.WriteFile(path, []byte("hello from mcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewStdioClient(ctx, StdioConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPStdioFixture"},
		Env: map[string]string{
			"UB_MCP_STDIO_FIXTURE": "1",
		},
	})
	if err != nil {
		t.Fatalf("NewStdioClient: %v", err)
	}
	defer c.Close()

	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "read_file" {
		t.Fatalf("tools = %#v, want read_file", tools)
	}

	args, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.CallTool(ctx, "read_file", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := res.Text(); got != "hello from mcp\n" {
		t.Fatalf("tool text = %q", got)
	}
}

func TestMCPStdioFixture(t *testing.T) {
	if os.Getenv("UB_MCP_STDIO_FIXTURE") != "1" {
		return
	}
	runMCPStdioFixture()
	os.Exit(0)
}

func runMCPStdioFixture() {
	r := bufio.NewReader(os.Stdin)
	for {
		body, err := readFrame(r)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return
		}
		if req.ID == nil {
			continue
		}
		switch req.Method {
		case "initialize":
			respondFixture(*req.ID, map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fixture", "version": "test"},
			})
		case "tools/list":
			respondFixture(*req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "read_file",
					"description": "Read a file",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
						"required": []string{"path"},
					},
				}},
			})
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			var args struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(params.Arguments, &args)
			content, err := os.ReadFile(args.Path)
			if err != nil {
				respondFixtureError(*req.ID, -32000, err.Error())
				continue
			}
			respondFixture(*req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": string(content)}},
			})
		default:
			respondFixtureError(*req.ID, -32601, "method not found")
		}
	}
}

func respondFixture(id int64, result any) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	_ = writeFrame(os.Stdout, payload)
}

func respondFixtureError(id int64, code int, message string) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	_ = writeFrame(os.Stdout, payload)
}
