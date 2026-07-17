package greeting

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting(); got != "Hello" {
		t.Fatalf("Greeting() = %q, want Hello", got)
	}
}
