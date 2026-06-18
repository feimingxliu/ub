package tui

import "testing"

// The compaction lifecycle emits a "running" notice when it starts compacting
// and a "done" notice when it finishes. The TUI updates the running notice in
// place via appendOrUpdateBlock, which keys on role+key — so both notices must
// produce the SAME key, or the "compacting" status chip never clears.
//
// Keying used to rely on Summary text containing "compact", which broke when
// prepareMessages emitted "summarized N earlier messages". Pin the stable,
// struct-driven keying here.
func TestCompactingNoticeRunningAndDoneShareKey(t *testing.T) {
	running := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "notice",
		Notice:       "compacting",
		Status:       "running",
		Summary:      "compacting context",
	})
	done := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "notice",
		Notice:       "compacting",
		Status:       "done",
		Summary:      "compacted 3 earlier messages",
	})

	const wantKey = "notice:compacting"
	if running.key != wantKey {
		t.Fatalf("running notice key = %q, want %q", running.key, wantKey)
	}
	if done.key != wantKey {
		t.Fatalf("done notice key = %q, want %q", done.key, wantKey)
	}
	if running.status != "running" {
		t.Fatalf("running notice status = %q, want running", running.status)
	}
	if done.status != "done" {
		t.Fatalf("done notice status = %q, want done", done.status)
	}

	// A notice without a known Notice kind must not pick up a key, so unrelated
	// notices (e.g. auto-memory) never collide with the compacting key.
	other := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "notice",
		Status:       "done",
		Summary:      "auto memory skipped",
	})
	if other.key != "" {
		t.Fatalf("unrelated notice key = %q, want empty", other.key)
	}
}

// Even if the done summary wording drifts away from "compact", keying must hold
// because it is driven by the Notice field, not summary text.
func TestCompactingNoticeKeyIgnoresSummaryWording(t *testing.T) {
	done := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "notice",
		Notice:       "compacting",
		Status:       "done",
		Summary:      "summarized earlier turns", // no "compact" substring
	})
	if done.key != "notice:compacting" {
		t.Fatalf("done notice key = %q, want notice:compacting regardless of wording", done.key)
	}
}
