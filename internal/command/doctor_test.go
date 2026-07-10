package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	mcptool "github.com/feimingxliu/ub/internal/tool/mcp"
)

func TestDoctorChecksCompatProviderAndCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  compat:
    type: openai-compat
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	tc := newTestRootCommand("doctor", "--plain")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	output := out.String()
	for _, want := range []string{"compat", "reachable", "local-model", "rg", "gopls", "typescript-language-server", "npx"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorReportsMCPStatus(t *testing.T) {
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
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
				}},
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

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
mcp_servers:
  docs:
    type: http
    url: `+server.URL+`
`)
	t.Chdir(temp)

	tc := newTestRootCommand("doctor", "--plain")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	output := out.String()
	for _, want := range []string{"mcp:", "docs", "http", "connected", "tools\t1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorUsesDevProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"dev-model"}]}`))
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `profiles:
  dev:
    providers:
      compat:
        type: openai-compat
        base_url: `+server.URL+`
`)
	t.Chdir(temp)

	tc := newTestRootCommand("--dev", "doctor", "--plain")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor --dev: %v", err)
	}
	if !strings.Contains(out.String(), "dev-model") {
		t.Fatalf("doctor did not use dev profile:\n%s", out.String())
	}
}

func TestDoctorPlainDisablesStyledOutput(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)

	tc := newTestRootCommand("doctor")
	styled := tc.out
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor styled: %v", err)
	}
	if !strings.Contains(styled.String(), "\x1b[") {
		t.Fatalf("styled doctor output has no ANSI sequences:\n%s", styled.String())
	}

	tc = newTestRootCommand("doctor", "--plain")
	plain := tc.out
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor plain: %v", err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("plain doctor output contains ANSI sequences:\n%s", plain.String())
	}
}

func TestDoctorJSONReportsMachineReadableOutput(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)

	tc := newTestRootCommand("doctor", "--json")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	var report struct {
		Providers []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"providers"`
		MCP []struct {
			Name string `json:"name"`
		} `json:"mcp"`
		Commands []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Path   string `json:"path,omitempty"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor json: %v\n%s", err, out.String())
	}
	if len(report.Providers) != 1 || report.Providers[0].Name != "fake" || report.Providers[0].Status != "offline" {
		t.Fatalf("providers = %#v", report.Providers)
	}
	if len(report.MCP) != 0 {
		t.Fatalf("mcp = %#v, want empty when no servers configured", report.MCP)
	}
	if len(report.Commands) == 0 {
		t.Fatalf("commands should be present: %#v", report.Commands)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("json doctor output contains ANSI sequences:\n%s", out.String())
	}
}

func TestDoctorOpenAIMissingKeyDoesNotProbe(t *testing.T) {
	probed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed = true
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  openai:
    type: openai
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	tc := newTestRootCommand("doctor", "--plain")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if probed {
		t.Fatal("doctor should not probe openai without api_key")
	}
	if !strings.Contains(out.String(), "NO_API_KEY") {
		t.Fatalf("missing NO_API_KEY:\n%s", out.String())
	}
}

func TestDoctorSuggest(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "xdg"))
	t.Chdir(temp)

	tc := newTestRootCommand("doctor", "--plain", "--suggest")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("doctor --suggest: %v", err)
	}
	for _, want := range []string{"suggested dev profile", "profiles:", "dev:", "execution_mode: plan"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("suggest output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorPrefersLiveStatusOverProbe(t *testing.T) {
	live := []mcptool.ServerStatus{
		{Name: "live-server", Type: "http", Status: "connected"},
		{Name: "dead-server", Type: "stdio", Status: "backoff", Err: fmt.Errorf("connection refused")},
	}
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"fake": {Type: "fake"},
		},
		MCPServers: map[string]config.MCPServerConfig{
			"live-server": {Type: "http", URL: "http://unused.example.com"},
			"dead-server": {Type: "stdio", Command: "unused"},
		},
	}
	report := collectDoctorReport(context.Background(), cfg, false, live)
	if len(report.MCP) != 2 {
		t.Fatalf("MCP entries = %d, want 2", len(report.MCP))
	}
	// The live-server should be reported as "connected" from live status,
	// not probed (which would fail since the URL is unreachable).
	if report.MCP[0].Name != "live-server" || report.MCP[0].Status != "connected" {
		t.Errorf("live-server = %+v, want connected", report.MCP[0])
	}
	if report.MCP[1].Name != "dead-server" || report.MCP[1].Status != "backoff" {
		t.Errorf("dead-server = %+v, want backoff", report.MCP[1])
	}
	if report.MCP[1].Error == "" {
		t.Error("dead-server should have error message")
	}
}
