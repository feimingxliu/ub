package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/config"
)

func TestClientLifecycleAndDocumentSync(t *testing.T) {
	logPath := t.TempDir() + "/lsp.log"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Start(ctx, ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestLSPFixture"},
		Env: map[string]string{
			"UB_LSP_FIXTURE":     "1",
			"UB_LSP_FIXTURE_LOG": logPath,
		},
		Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	file := t.TempDir() + "/main.go"
	if err := c.DidOpen(ctx, file, "package main\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	if err := c.DidChange(ctx, file, "package main\nfunc main() {}\n"); err != nil {
		t.Fatalf("DidChange: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logBytes)
	for _, method := range []string{
		"initialize",
		"initialized",
		"textDocument/didOpen",
		"textDocument/didChange",
		"shutdown",
		"exit",
	} {
		if !strings.Contains(log, method) {
			t.Fatalf("log missing %s:\n%s", method, log)
		}
	}
}

func TestManagerDidChangeFileRoutesByFileType(t *testing.T) {
	root := t.TempDir()
	file := root + "/main.go"
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := root + "/lsp.log"
	t.Setenv("UB_LSP_FIXTURE", "1")
	t.Setenv("UB_LSP_FIXTURE_LOG", logPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, warnings := StartConfigured(ctx, root, map[string]config.LSPServerConfig{
		"fixture": {
			Command:   os.Args[0],
			Args:      []string{"-test.run=TestLSPFixture"},
			FileTypes: []string{"go"},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %#v", warnings)
	}
	if m == nil {
		t.Fatalf("manager is nil")
	}
	if err := m.DidChangeFile(ctx, file); err != nil {
		t.Fatalf("DidChangeFile: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logBytes), "textDocument/didOpen") {
		t.Fatalf("manager did not open changed file:\n%s", logBytes)
	}
}

func TestManagerDiagnosticsAndReferences(t *testing.T) {
	root := t.TempDir()
	file := root + "/main.go"
	if err := os.WriteFile(file, []byte("package main\nfunc main() {"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UB_LSP_FIXTURE", "1")
	t.Setenv("UB_LSP_FIXTURE_LOG", root+"/lsp.log")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, warnings := StartConfigured(ctx, root, map[string]config.LSPServerConfig{
		"fixture": {
			Command:   os.Args[0],
			Args:      []string{"-test.run=TestLSPFixture"},
			FileTypes: []string{"go"},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %#v", warnings)
	}
	defer m.Close()

	diags, err := m.Diagnostics(ctx, file)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diags) != 1 || len(diags[0].Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one diagnostic", diags)
	}
	refs, err := m.References(ctx, file, 1, 1)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(refs) != 1 || refs[0].Range.Start.Line != 0 {
		t.Fatalf("references = %#v, want one location", refs)
	}
}

func TestLSPFixture(t *testing.T) {
	if os.Getenv("UB_LSP_FIXTURE") != "1" {
		return
	}
	runLSPFixture()
	os.Exit(0)
}

func runLSPFixture() {
	log, err := os.OpenFile(os.Getenv("UB_LSP_FIXTURE_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer log.Close()
	r := bufio.NewReader(os.Stdin)
	for {
		body, err := readFrame(r)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			return
		}
		fmt.Fprintln(log, msg.Method)
		switch msg.Method {
		case "initialize":
			respondLSP(msg.ID, map[string]any{"capabilities": map[string]any{}})
		case "shutdown":
			respondLSP(msg.ID, nil)
		case "textDocument/didOpen", "textDocument/didChange":
			publishFixtureDiagnostics(msg.Params)
		case "textDocument/references":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			respondLSP(msg.ID, []map[string]any{{
				"uri": params.TextDocument.URI,
				"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   map[string]any{"line": 0, "character": 4},
				},
			}})
		case "exit":
			return
		}
	}
}

func respondLSP(id *int64, result any) {
	if id == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      *id,
		"result":  result,
	})
	_ = writeFrame(os.Stdout, payload)
}

func publishFixtureDiagnostics(raw json.RawMessage) {
	var uri string
	var openParams struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &openParams); err == nil {
		uri = openParams.TextDocument.URI
	}
	if uri == "" {
		var changeParams struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(raw, &changeParams)
		uri = changeParams.TextDocument.URI
	}
	if uri == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params": map[string]any{
			"uri": uri,
			"diagnostics": []map[string]any{{
				"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   map[string]any{"line": 0, "character": 1},
				},
				"severity": 1,
				"source":   "fixture",
				"message":  "syntax error",
			}},
		},
	})
	_ = writeFrame(os.Stdout, payload)
}
