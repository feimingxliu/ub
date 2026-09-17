package reasoning

import "testing"

func TestNormalizeMax(t *testing.T) {
	got, err := NormalizeEffort(" MAX ")
	if err != nil || got != EffortMax {
		t.Fatalf("effort=%q err=%v", got, err)
	}
	if _, err := NormalizeEffort("unsupported"); err == nil {
		t.Fatal("unknown effort accepted")
	}
}
