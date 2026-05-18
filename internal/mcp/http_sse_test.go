package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientListsAndCallsTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writeHTTPMCPResponse(t, w, req)
	}))
	defer srv.Close()

	c, err := NewHTTPClient(HTTPConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v, want echo", tools)
	}
	res, err := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := res.Text(); got != "hi" {
		t.Fatalf("result text = %q, want hi", got)
	}
}

func TestSSEClientListsTools(t *testing.T) {
	messages := make(chan []byte, 8)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("response writer is not flusher")
			return
		}
		fmt.Fprint(w, "event: endpoint\n")
		fmt.Fprint(w, "data: /message\n\n")
		flusher.Flush()
		for {
			select {
			case msg := <-messages:
				fmt.Fprint(w, "event: message\n")
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if req.ID == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		payload := makeMCPResponse(t, req)
		messages <- payload
		w.WriteHeader(http.StatusAccepted)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewSSEClient(ctx, SSEConfig{URL: srv.URL + "/sse"})
	if err != nil {
		t.Fatalf("NewSSEClient: %v", err)
	}
	defer c.Close()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v, want echo", tools)
	}
}

func writeHTTPMCPResponse(t *testing.T, w http.ResponseWriter, req rpcRequest) {
	t.Helper()
	if req.ID == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(makeMCPResponse(t, req)); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func makeMCPResponse(t *testing.T, req rpcRequest) []byte {
	t.Helper()
	var result any
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fixture", "version": "test"},
		}
	case "tools/list":
		result = map[string]any{
			"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo text",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"text": map[string]any{"type": "string"}},
				},
			}},
		}
	case "tools/call":
		var params callToolParams
		if err := remarshal(req.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		var args struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			t.Fatalf("decode args: %v", err)
		}
		result = map[string]any{"content": []map[string]any{{"type": "text", "text": args.Text}}}
	default:
		return mustJSON(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   map[string]any{"code": -32601, "message": "method not found"},
		})
	}
	return mustJSON(t, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func remarshal(in any, out any) error {
	buf, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, out)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return buf
}
