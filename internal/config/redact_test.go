package config

import "testing"

func TestRedactMasksSecretsAndLeavesOriginalUntouched(t *testing.T) {
	cfg := &Config{
		DefaultModel: "anthropic/claude-sonnet-4-7",
		Providers: map[string]ProviderConfig{
			"anthropic": {
				Type:    "anthropic",
				APIKey:  "sk-real-key",
				BaseURL: "https://example.test",
			},
		},
		Unknown: map[string]any{
			"profiles": map[string]any{"dev": "ignored"},
		},
	}

	got := Redact(cfg)
	if got.Providers["anthropic"].APIKey != redactedMask {
		t.Fatalf("redacted api_key = %q", got.Providers["anthropic"].APIKey)
	}
	if got.Providers["anthropic"].BaseURL != "https://example.test" {
		t.Fatalf("base_url changed: %q", got.Providers["anthropic"].BaseURL)
	}
	if cfg.Providers["anthropic"].APIKey != "sk-real-key" {
		t.Fatalf("original config was mutated")
	}
	if got.Unknown["profiles"] == nil {
		t.Fatalf("unknown fields were not copied")
	}
}

func TestRedactNil(t *testing.T) {
	if Redact(nil) != nil {
		t.Fatal("Redact(nil) should return nil")
	}
}
