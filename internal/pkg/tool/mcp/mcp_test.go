package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func TestRegisterConfiguredRegistersPrefixedToolsAndKeepsGoingAfterFailure(t *testing.T) {
	one := newMCPHTTPServer(t, "echo")
	defer one.Close()
	two := newMCPHTTPServer(t, "echo")
	defer two.Close()

	reg := tool.New()
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"one": {Type: "http", URL: one.URL},
		"two": {Type: "http", URL: two.URL},
		"bad": {Type: "http", URL: "http://127.0.0.1:1/not-listening"},
	})
	defer conns.Close()

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
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"remote": {Type: "http", URL: server.URL},
	})
	defer conns.Close()
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
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"remote": {Type: "http", URL: server.URL},
	})
	defer conns.Close()
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

func TestToolReconnectsAfterIdleClose(t *testing.T) {
	oldIdleTimeout := serverConnectionIdleTimeout
	serverConnectionIdleTimeout = 10 * time.Millisecond
	t.Cleanup(func() { serverConnectionIdleTimeout = oldIdleTimeout })

	var initializes atomic.Int32
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
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "after idle"}},
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
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"remote": {Type: "http", URL: server.URL},
	})
	defer conns.Close()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	tl, ok := reg.Get("mcp__remote__echo")
	if !ok {
		t.Fatalf("missing mcp__remote__echo")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		statuses := conns.Status()
		if len(statuses) == 1 && statuses[0].Status == "disconnected" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	statuses := conns.Status()
	if len(statuses) != 1 || statuses[0].Status != "disconnected" {
		t.Fatalf("status after idle = %#v, want disconnected", statuses)
	}

	result, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute after idle: %v", err)
	}
	if result.Content != "after idle" {
		t.Fatalf("content = %q, want after idle", result.Content)
	}
	if initializes.Load() < 2 {
		t.Fatalf("initialize count = %d, want reconnect after idle", initializes.Load())
	}
}

func TestConnectionsStatusReportsConnected(t *testing.T) {
	srv := newMCPHTTPServer(t, "echo")
	defer srv.Close()

	reg := tool.New()
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"test": {Type: "http", URL: srv.URL},
	})
	defer conns.Close()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	statuses := conns.Status()
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	s := statuses[0]
	if s.Name != "test" {
		t.Errorf("name = %q, want test", s.Name)
	}
	if s.Type != "http" {
		t.Errorf("type = %q, want http", s.Type)
	}
	if s.Status != "connected" {
		t.Errorf("status = %q, want connected", s.Status)
	}
}

func TestConnectionsStatusIncludesStartupFailures(t *testing.T) {
	srv := newMCPHTTPServer(t, "echo")
	defer srv.Close()

	reg := tool.New()
	conns, warnings := RegisterConfigured(context.Background(), reg, map[string]config.MCPServerConfig{
		"bad":  {Type: "http", URL: "http://127.0.0.1:1/not-listening"},
		"good": {Type: "http", URL: srv.URL},
	})
	defer conns.Close()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one startup warning", warnings)
	}

	statuses := conns.Status()
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2: %#v", len(statuses), statuses)
	}
	if statuses[0].Name != "bad" || statuses[0].Status != "error" || statuses[0].Err == nil {
		t.Fatalf("bad status = %#v, want startup error", statuses[0])
	}
	if statuses[1].Name != "good" || statuses[1].Status != "connected" || statuses[1].ToolCount != 1 {
		t.Fatalf("good status = %#v, want connected with tool count", statuses[1])
	}
}

func TestConnectionsStatusReportsDisconnectedAndBackoff(t *testing.T) {
	conn := newServerConnection("dead", config.MCPServerConfig{Type: "http", URL: "http://127.0.0.1:1/not-listening"}, connectMCPServer)

	// Initial state: no client, no error: disconnected.
	s := conn.Status()
	if s.Status != "disconnected" {
		t.Fatalf("initial status = %q, want disconnected", s.Status)
	}

	// Simulate a failed connect that set backoff.
	conn.markDisconnected(fmt.Errorf("connection refused"))
	s = conn.Status()
	if s.Status != "backoff" {
		t.Fatalf("after disconnect status = %q, want backoff", s.Status)
	}
	if s.Err == nil {
		t.Fatal("expected non-nil Err after disconnect")
	}

	// Expire the backoff timer.
	conn.mu.Lock()
	conn.nextAttempt = time.Now().Add(-time.Second)
	conn.mu.Unlock()
	s = conn.Status()
	if s.Status != "disconnected" {
		t.Fatalf("after backoff expiry status = %q, want disconnected", s.Status)
	}
}

func TestConnectionsStatusEmptyWhenNoServers(t *testing.T) {
	reg := tool.New()
	conns, _ := RegisterConfigured(context.Background(), reg, nil)
	defer conns.Close()
	if got := conns.Status(); got != nil {
		t.Fatalf("expected nil statuses, got %v", got)
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
