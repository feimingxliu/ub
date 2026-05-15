package slash

import "testing"

func TestParse(t *testing.T) {
	cmd, err := Parse("/model openai/gpt-4o-mini")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cmd.Name != "model" || len(cmd.Args) != 1 || cmd.Args[0] != "openai/gpt-4o-mini" {
		t.Fatalf("command = %#v", cmd)
	}
}

func TestParseErrors(t *testing.T) {
	for _, input := range []string{"hello", "/", "/unknown"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("Parse(%q) returned nil error", input)
			}
		})
	}
}
