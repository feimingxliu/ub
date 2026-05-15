package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor", "--plain"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	output := out.String()
	for _, want := range []string{"compat", "reachable", "local-model", "rg", "gopls", "typescript-language-server", "npx"} {
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

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--dev", "doctor", "--plain"})

	if err := cmd.Execute(); err != nil {
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

	cmd := newRootCmd()
	styled := &bytes.Buffer{}
	cmd.SetOut(styled)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor styled: %v", err)
	}
	if !strings.Contains(styled.String(), "\x1b[") {
		t.Fatalf("styled doctor output has no ANSI sequences:\n%s", styled.String())
	}

	cmd = newRootCmd()
	plain := &bytes.Buffer{}
	cmd.SetOut(plain)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor", "--plain"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor plain: %v", err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("plain doctor output contains ANSI sequences:\n%s", plain.String())
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

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor", "--plain"})

	if err := cmd.Execute(); err != nil {
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

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor", "--plain", "--suggest"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor --suggest: %v", err)
	}
	for _, want := range []string{"suggested dev profile", "profiles:", "dev:", "execution_mode: plan"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("suggest output missing %q:\n%s", want, out.String())
		}
	}
}
