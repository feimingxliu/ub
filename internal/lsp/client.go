package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type documentState struct {
	URI        string
	LanguageID string
	Version    int
	Open       bool
}

// Client is a minimal stdio LSP client.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	root   string

	nextID atomic.Int64
	write  sync.Mutex

	pendingMu sync.Mutex
	pending   map[int64]chan rpcMessage

	docsMu sync.Mutex
	docs   map[string]*documentState

	diagnosticsMu sync.RWMutex
	diagnostics   map[string][]Diagnostic

	done      chan error
	closeOnce sync.Once
	closeErr  error
}

// Start starts and initializes one stdio LSP server.
func Start(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("lsp: command is required")
	}
	root := cfg.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("lsp: get cwd: %w", err)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %q: %w", cfg.Command, err)
	}
	c := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		root:        root,
		pending:     map[int64]chan rpcMessage{},
		docs:        map[string]*documentState{},
		diagnostics: map[string][]Diagnostic{},
		done:        make(chan error, 1),
	}
	c.nextID.Store(1)
	go c.readLoop()
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) initialize(ctx context.Context) error {
	rootURI, err := fileURI(c.root)
	if err != nil {
		return err
	}
	var out struct {
		Capabilities map[string]any `json:"capabilities,omitempty"`
	}
	if err := c.Call(ctx, "initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"dynamicRegistration": false,
					"didSave":             false,
				},
			},
		},
	}, &out); err != nil {
		return err
	}
	return c.Notify(ctx, "initialized", map[string]any{})
}

// Call sends one LSP request and decodes its result into out.
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan rpcMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	payload, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	if err := c.writeFrame(payload); err != nil {
		return err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return msg.Error
		}
		if out == nil {
			return nil
		}
		if len(msg.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(msg.Result, out); err != nil {
			return fmt.Errorf("lsp: decode %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Notify sends one LSP notification.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	if err := c.writeFrame(payload); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (c *Client) writeFrame(payload []byte) error {
	c.write.Lock()
	defer c.write.Unlock()
	return writeFrame(c.stdin, payload)
}

func (c *Client) readLoop() {
	for {
		body, err := readFrame(c.stdout)
		if err != nil {
			c.done <- err
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if msg.ID != nil && msg.Method == "" {
			c.pendingMu.Lock()
			ch := c.pending[*msg.ID]
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Method == "textDocument/publishDiagnostics" {
			c.recordDiagnostics(msg.Params)
		}
	}
}

// DidOpen sends textDocument/didOpen for path.
func (c *Client) DidOpen(ctx context.Context, path, text string) error {
	uri, err := fileURI(path)
	if err != nil {
		return err
	}
	c.docsMu.Lock()
	doc := c.docs[uri]
	if doc == nil {
		doc = &documentState{URI: uri, LanguageID: languageID(path), Version: 1, Open: true}
		c.docs[uri] = doc
	} else {
		doc.Open = true
		doc.Version++
	}
	version := doc.Version
	lang := doc.LanguageID
	c.docsMu.Unlock()
	return c.Notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": lang,
			"version":    version,
			"text":       text,
		},
	})
}

// DidChange sends textDocument/didChange for path, opening it first if needed.
func (c *Client) DidChange(ctx context.Context, path, text string) error {
	uri, err := fileURI(path)
	if err != nil {
		return err
	}
	c.docsMu.Lock()
	doc := c.docs[uri]
	if doc == nil || !doc.Open {
		c.docsMu.Unlock()
		return c.DidOpen(ctx, path, text)
	}
	doc.Version++
	version := doc.Version
	c.docsMu.Unlock()
	return c.Notify(ctx, "textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]any{{
			"text": text,
		}},
	})
}

// Close shuts down the LSP server and releases the child process.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = c.Call(ctx, "shutdown", nil, nil)
		_ = c.Notify(ctx, "exit", nil)
		_ = c.stdin.Close()
		select {
		case err := <-c.done:
			if err != nil && err != io.EOF {
				c.closeErr = err
			}
		case <-time.After(closeTimeout):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			c.closeErr = <-c.done
		}
		_ = c.cmd.Wait()
	})
	return c.closeErr
}

func (c *Client) recordDiagnostics(raw json.RawMessage) {
	var params struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.URI == "" {
		return
	}
	c.diagnosticsMu.Lock()
	c.diagnostics[params.URI] = append([]Diagnostic(nil), params.Diagnostics...)
	c.diagnosticsMu.Unlock()
}

func (c *Client) diagnosticsFor(uri string) ([]Diagnostic, bool) {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()
	diags, ok := c.diagnostics[uri]
	if !ok {
		return nil, false
	}
	return append([]Diagnostic(nil), diags...), true
}

func (c *Client) allDiagnostics() []FileDiagnostics {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()
	out := make([]FileDiagnostics, 0, len(c.diagnostics))
	for uri, diags := range c.diagnostics {
		out = append(out, FileDiagnostics{
			URI:         uri,
			Path:        pathFromURI(uri),
			Diagnostics: append([]Diagnostic(nil), diags...),
		})
	}
	return out
}
