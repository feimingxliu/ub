package config

import "testing"

func TestRedactMasksSecretsAndLeavesOriginalUntouched(t *testing.T) {
	webEnabled := true
	cfg := &Config{
		DefaultModel: "anthropic/claude-sonnet-4-7",
		Providers: map[string]ProviderConfig{
			"anthropic": {
				Type:    "anthropic",
				APIKey:  "sk-real-key",
				BaseURL: "https://example.test",
				Headers: map[string]string{
					"Authorization":     "Bearer sk-real-key",
					"anthropic-version": "2023-06-01",
				},
			},
		},
		Profiles: map[string]ProfileConfig{
			"dev": {
				Providers: map[string]ProviderConfig{
					"openai": {
						Type:   "openai",
						APIKey: "sk-profile-key",
					},
				},
			},
		},
		Tools: ToolsConfig{
			Web: WebToolConfig{
				Enabled:  &webEnabled,
				Provider: "brave",
				APIKey:   "web-real-key",
			},
		},
		Unknown: map[string]any{
			"future": map[string]any{"dev": "ignored"},
		},
	}

	got := Redact(cfg)
	if got.Providers["anthropic"].APIKey != redactedMask {
		t.Fatalf("redacted api_key = %q", got.Providers["anthropic"].APIKey)
	}
	if got.Providers["anthropic"].BaseURL != "https://example.test" {
		t.Fatalf("base_url changed: %q", got.Providers["anthropic"].BaseURL)
	}
	if got.Providers["anthropic"].Headers["Authorization"] != redactedMask {
		t.Fatalf("authorization header = %q", got.Providers["anthropic"].Headers["Authorization"])
	}
	if got.Providers["anthropic"].Headers["anthropic-version"] != "2023-06-01" {
		t.Fatalf("non-secret header changed: %q", got.Providers["anthropic"].Headers["anthropic-version"])
	}
	if cfg.Providers["anthropic"].APIKey != "sk-real-key" {
		t.Fatalf("original config was mutated")
	}
	if cfg.Providers["anthropic"].Headers["Authorization"] != "Bearer sk-real-key" {
		t.Fatalf("original headers were mutated")
	}
	if got.Profiles["dev"].Providers["openai"].APIKey != redactedMask {
		t.Fatalf("profile api_key = %q", got.Profiles["dev"].Providers["openai"].APIKey)
	}
	if got.Tools.Web.APIKey != redactedMask {
		t.Fatalf("web api_key = %q", got.Tools.Web.APIKey)
	}
	if got.Unknown["future"] == nil {
		t.Fatalf("unknown fields were not copied")
	}
}

func TestRedactNil(t *testing.T) {
	if Redact(nil) != nil {
		t.Fatal("Redact(nil) should return nil")
	}
}
