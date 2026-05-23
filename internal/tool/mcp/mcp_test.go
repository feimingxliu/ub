package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestRegisterConfiguredRegistersPrefixedToolsAndKeepsGoingAfterFailure(t *testing.T) {
	one := newMCPHTTPServer(t, "echo")
	defer one.Close()
	two := newMCPHTTPServer(t, "echo")
	defer two.Close()

	reg := tool.New()
	closeFn, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"one": {Type: "http", URL: one.URL},
		"two": {Type: "http", URL: two.URL},
		"bad": {Type: "http", URL: "http://127.0.0.1:1/not-listening"},
	})
	defer closeFn()

	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1: %#v", len(warnings), warnings)
	}
	if _, ok := reg.Get("mcp__one__echo"); !ok {
		t.Fatalf("missing mcp__one__echo")
	}
	tl, ok := reg.Get("mcp__two__echo")
	if !ok {
		t.Fatalf("missing mcp__two__echo")
	}
	if tl.Risk() != tool.RiskExec {
		t.Fatalf("risk = %q, want exec", tl.Risk())
	}
	result, err := tl.Execute(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("content = %q, want hello", result.Content)
	}
}

func TestToolReconnectsAndRetriesAfterCallFailure(t *testing.T) {
	var initializes atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64           `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			initializes.Add(1)
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
				}},
			}
		case "tools/call":
			if calls.Add(1) == 1 {
				http.Error(w, "temporary disconnect", http.StatusInternalServerError)
				return
			}
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "reconnected"}},
			}
		default:
			t.Errorf("unexpected method %q", req.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	reg := tool.New()
	closeFn, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"remote": {Type: "http", URL: server.URL},
	})
	defer closeFn()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	tl, ok := reg.Get("mcp__remote__echo")
	if !ok {
		t.Fatalf("missing mcp__remote__echo")
	}
	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute after reconnect: %v", err)
	}
	if result.Content != "reconnected" {
		t.Fatalf("content = %q, want reconnected", result.Content)
	}
	if calls.Load() != 2 {
		t.Fatalf("tool calls = %d, want 2", calls.Load())
	}
	if initializes.Load() < 2 {
		t.Fatalf("initialize count = %d, want reconnect initialize", initializes.Load())
	}
}

func TestToolDoesNotReconnectOnServerError(t *testing.T) {
	var initializes atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id,omitempty"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var (
			result any
			rpcErr map[string]any
		)
		switch req.Method {
		case "initialize":
			initializes.Add(1)
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
				}},
			}
		case "tools/call":
			calls.Add(1)
			rpcErr = map[string]any{"code": -32602, "message": "invalid arguments"}
		default:
			t.Errorf("unexpected method %q", req.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	reg := tool.New()
	closeFn, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"remote": {Type: "http", URL: server.URL},
	})
	defer closeFn()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	tl, ok := reg.Get("mcp__remote__echo")
	if !ok {
		t.Fatalf("missing mcp__remote__echo")
	}
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatalf("Execute: expected server error, got nil")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("tool calls = %d, want 1 (no retry on server error)", got)
	}
	if got := initializes.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want 1 (no reconnect on server error)", got)
	}
}

func newMCPHTTPServer(t *testing.T, toolName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64           `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{{
					"name":        toolName,
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
				}},
			}
		case "tools/call":
			var params struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("decode params: %v", err)
				return
			}
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				t.Errorf("decode args: %v", err)
				return
			}
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": args.Text}},
			}
		default:
			t.Errorf("unexpected method %q", req.Method)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}
