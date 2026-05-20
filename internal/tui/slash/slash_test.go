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

func TestParseExit(t *testing.T) {
	cmd, err := Parse("/exit")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cmd.Name != "exit" {
		t.Fatalf("command = %#v, want exit", cmd)
	}
}

func TestParseCompact(t *testing.T) {
	cmd, err := Parse("/compact")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cmd.Name != "compact" || len(cmd.Args) != 0 {
		t.Fatalf("command = %#v, want compact", cmd)
	}
}

func TestParseNew(t *testing.T) {
	cmd, err := Parse("/new")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cmd.Name != "new" || len(cmd.Args) != 0 {
		t.Fatalf("command = %#v, want new", cmd)
	}
}

func TestParseApprovalModel(t *testing.T) {
	cmd, err := Parse("/approval-model fake/reviewer")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cmd.Name != "approval-model" || len(cmd.Args) != 1 || cmd.Args[0] != "fake/reviewer" {
		t.Fatalf("command = %#v, want approval-model fake/reviewer", cmd)
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

func TestMatchReturnsUsage(t *testing.T) {
	matches := Match("/m")
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want model and mode", matches)
	}
	if matches[0].Usage != "/model [model]" || matches[1].Usage != "/mode <work|plan|auto>" {
		t.Fatalf("matches = %#v", matches)
	}
}
