package cli

import (
	"testing"
	"time"
)

func TestEffectiveTUIEventTimeoutDisabledByDefault(t *testing.T) {
	if got := effectiveTUIEventTimeout(2 * time.Minute); got != 0 {
		t.Fatalf("effectiveTUIEventTimeout = %s, want disabled", got)
	}
}
