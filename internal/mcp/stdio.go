package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

func newStdioTransport(ctx context.Context, cfg StdioConfig) (*stdioTransport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp stdio: command is required")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start %q: %w", cfg.Command, err)
	}
	return &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

func (t *stdioTransport) Call(_ context.Context, req rpcRequest) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := writeFrame(t.stdin, payload); err != nil {
		return nil, fmt.Errorf("mcp stdio: write %s: %w", req.Method, err)
	}
	for {
		body, err := readFrame(t.stdout)
		if err != nil {
			return nil, fmt.Errorf("mcp stdio: read %s response: %w", req.Method, err)
		}
		var resp rpcResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("mcp stdio: decode response: %w", err)
		}
		if resp.ID == nil {
			continue
		}
		if *resp.ID != req.ID {
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) Notify(_ context.Context, method string, params any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	payload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	if err := writeFrame(t.stdin, payload); err != nil {
		return fmt.Errorf("mcp stdio: notify %s: %w", method, err)
	}
	return nil
}

func (t *stdioTransport) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		_ = t.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()
		select {
		case err := <-done:
			t.closeErr = err
		case <-time.After(2 * time.Second):
			if t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
			t.closeErr = <-done
		}
	})
	return t.closeErr
}
