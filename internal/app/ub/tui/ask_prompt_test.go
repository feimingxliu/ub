package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
)

// askKey builds a KeyPressMsg for a special key (arrows, enter, esc, space,
// tab, backspace) the way Bubble Tea reports it.
func askKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// askType builds a KeyPressMsg carrying printable text, matching how Bubble
// Tea delivers typed runes.
func askType(text string) tea.KeyPressMsg {
	r := []rune(text)
	if len(r) == 0 {
		return tea.KeyPressMsg(tea.Key{})
	}
	return tea.KeyPressMsg(tea.Key{Code: r[0], Text: text})
}

func twoQuestionAskRequest() agent.AskRequest {
	return agent.AskRequest{Questions: []agent.AskQuestion{
		{
			Header:   "Backend",
			Question: "Which store should ub use?",
			Options: []agent.AskOption{
				{Label: "SQLite", Description: "local durable store"},
				{Label: "Postgres", Description: "shared server"},
			},
		},
		{
			Header:   "Logging",
			Question: "Where should logs go?",
			Options: []agent.AskOption{
				{Label: "Console", Description: "stderr"},
				{Label: "File", Description: "rotating file"},
			},
		},
	}}
}

func TestAskPromptStepwiseAdvanceMultiQuestion(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())

	// Question 1: pick Postgres (option index 1) then Enter should advance,
	// not submit.
	m.HandleKey(askKey(tea.KeyDown))
	if got := m.option; got != 1 {
		t.Fatalf("option = %d, want 1 after down", got)
	}
	if m.HandleEnter() {
		t.Fatal("Enter on first question should advance, not submit")
	}
	if m.question != 1 {
		t.Fatalf("question = %d, want 1 after advance", m.question)
	}

	// Question 2: pick File (option index 1) then Enter should submit.
	m.HandleKey(askKey(tea.KeyDown))
	if m.HandleEnter() == false {
		t.Fatal("Enter on last question should submit")
	}
	resp := m.SubmitResponse()
	if len(resp.Answers) != 2 {
		t.Fatalf("answers = %d, want 2", len(resp.Answers))
	}
	if got := resp.Answers[0].Selected[0].Label; got != "Postgres" {
		t.Fatalf("answer[0] = %q, want Postgres", got)
	}
	if got := resp.Answers[1].Selected[0].Label; got != "File" {
		t.Fatalf("answer[1] = %q, want File", got)
	}
}

func TestAskPromptOtherTextInputSingleSelect(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	// Navigate to the virtual Other slot (2 real options => Other is index 2).
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleKey(askKey(tea.KeyDown))
	if m.option != 2 {
		t.Fatalf("option = %d, want 2 (Other)", m.option)
	}
	// Selecting Other enters the text-input sub-mode.
	m.HandleKey(askKey(tea.KeySpace))
	if !m.otherMode {
		t.Fatal("expected otherMode after selecting Other")
	}
	// Type a custom answer.
	for _, r := range "my custom store" {
		m.HandleKey(askType(string(r)))
	}
	if got := m.otherText[0]; got != "my custom store" {
		t.Fatalf("otherText = %q, want %q", got, "my custom store")
	}
	// Enter finalizes the draft and, since this is the last question after
	// advancing past it, signals submit. Here question 1 advances to 2.
	if m.HandleEnter() {
		t.Fatal("Enter should advance past question 1, not submit")
	}
	if m.otherMode {
		t.Fatal("otherMode should clear after Enter finalizes draft")
	}
	// The Other answer is recorded for question 1.
	resp := m.response(false)
	if got := strings.TrimSpace(resp.Answers[0].Text); got != "my custom store" {
		t.Fatalf("answer[0].Text = %q, want %q", got, "my custom store")
	}
	if len(resp.Answers[0].Selected) != 0 {
		t.Fatalf("answer[0].Selected = %v, want empty", resp.Answers[0].Selected)
	}
	if got := m.Summary(resp); !strings.Contains(got, "Backend: my custom store") {
		t.Fatalf("summary = %q, want custom text", got)
	}
}

func TestAskPromptOtherTextInputMultiSelect(t *testing.T) {
	req := agent.AskRequest{Questions: []agent.AskQuestion{{
		Header:      "Features",
		Question:    "Which features?",
		MultiSelect: true,
		Options: []agent.AskOption{
			{Label: "A"},
			{Label: "B"},
		},
	}}}
	m := newAskPromptModel(req)
	// Pick real option A (index 0) via digit key.
	m.HandleKey(askType("1"))
	// Move to Other (index 2) and toggle it into text input.
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleKey(askKey(tea.KeySpace))
	if !m.otherMode {
		t.Fatal("expected otherMode after toggling Other")
	}
	for _, r := range "extra" {
		m.HandleKey(askType(string(r)))
	}
	// Finalize: question 0 is the last, so Enter submits.
	if !m.HandleEnter() {
		t.Fatal("Enter on last question should submit")
	}
	resp := m.response(false)
	if len(resp.Answers[0].Selected) != 1 || resp.Answers[0].Selected[0].Label != "A" {
		t.Fatalf("answer.Selected = %v, want [A]", resp.Answers[0].Selected)
	}
	if got := strings.TrimSpace(resp.Answers[0].Text); got != "extra" {
		t.Fatalf("answer.Text = %q, want extra", got)
	}
}

func TestAskPromptOtherInputEscExitsOnlyOtherMode(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	// Enter Other input mode and type something.
	m.option = 2
	m.HandleKey(askKey(tea.KeySpace))
	m.HandleKey(askType("draft"))
	if !m.otherMode {
		t.Fatal("expected otherMode")
	}
	// Esc is handled by the caller (model_update) via ExitOtherMode, not by
	// HandleKey — simulate that path here.
	m.ExitOtherMode()
	if m.otherMode {
		t.Fatal("ExitOtherMode should clear otherMode")
	}
	if got := m.otherText[0]; got != "draft" {
		t.Fatalf("draft = %q, want retained", got)
	}
	// The prompt is still alive (question unchanged, no submit).
	if m.question != 0 {
		t.Fatalf("question = %d, want 0 (no skip)", m.question)
	}
}

func TestAskPromptBackNavigateEditPrevious(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	m.HandleKey(askKey(tea.KeyDown)) // Postgres
	m.HandleEnter()                  // advance to question 2
	if m.question != 1 {
		t.Fatalf("question = %d, want 1", m.question)
	}
	// Go back to question 1 and change the selection to SQLite.
	m.HandleKey(askKey(tea.KeyLeft))
	if m.question != 0 {
		t.Fatalf("question = %d, want 0 after left", m.question)
	}
	m.option = 0 // SQLite
	m.HandleKey(askKey(tea.KeySpace))
	resp := m.response(false)
	if got := resp.Answers[0].Selected[0].Label; got != "SQLite" {
		t.Fatalf("answer[0] = %q, want SQLite after re-edit", got)
	}
}

func TestAskPromptLongQuestionWrapsNotTruncates(t *testing.T) {
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 6)
	req := agent.AskRequest{Questions: []agent.AskQuestion{{
		Header:   "Direction",
		Question: long,
		Options:  []agent.AskOption{{Label: "A"}, {Label: "B"}},
	}}}
	m := newAskPromptModel(req)
	view := m.View(40)
	if strings.Contains(view, "…") {
		t.Fatalf("view should not truncate, got ellipsis:\n%s", view)
	}
	if !strings.Contains(view, "the quick brown fox") {
		t.Fatalf("view should contain question text:\n%s", view)
	}
	if !strings.Contains(view, "Agent question (1/1)") {
		t.Fatalf("view should show step indicator:\n%s", view)
	}
}

func TestAskPromptLongOptionWrapsNotTruncates(t *testing.T) {
	long := strings.Repeat("verylongoptionword", 12)
	req := agent.AskRequest{Questions: []agent.AskQuestion{{
		Header:   "Pick",
		Question: "which?",
		Options: []agent.AskOption{
			{Label: long, Description: "desc " + long},
		},
	}}}
	m := newAskPromptModel(req)
	view := m.View(40)
	if strings.Contains(view, "…") {
		t.Fatalf("view should not truncate, got ellipsis:\n%s", view)
	}
	if !strings.Contains(view, "verylongoptionword") {
		t.Fatalf("view should contain option label:\n%s", view)
	}
}

func TestAskPromptOtherDraftRetainedAcrossNavigation(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	// Enter Other input on question 1, type a draft, then navigate away
	// without finalizing (left/right via moveQuestion resets otherMode).
	m.option = 2
	m.HandleKey(askKey(tea.KeySpace))
	m.HandleKey(askType("partial"))
	// While typing, arrow navigation is ignored so the draft isn't lost.
	// The user leaves Other input via Esc (ExitOtherMode) before navigating.
	m.ExitOtherMode()
	m.HandleKey(askKey(tea.KeyRight)) // advance to question 2
	if m.question != 1 {
		t.Fatalf("question = %d, want 1", m.question)
	}
	if m.otherMode {
		t.Fatal("navigating away should reset otherMode")
	}
	// Come back to question 1; the draft survives in otherText.
	m.HandleKey(askKey(tea.KeyLeft))
	if m.question != 0 {
		t.Fatalf("question = %d, want 0", m.question)
	}
	if got := m.otherText[0]; got != "partial" {
		t.Fatalf("draft = %q, want partial (retained)", got)
	}
}

func TestAskPromptSubmitCursorFallbackWhenOtherEmpty(t *testing.T) {
	req := agent.AskRequest{Questions: []agent.AskQuestion{{
		Header:   "Pick",
		Question: "which?",
		Options: []agent.AskOption{
			{Label: "First"},
			{Label: "Second"},
		},
	}}}
	m := newAskPromptModel(req)
	// Leave the cursor on the Other slot with an empty draft and submit.
	m.option = 2
	resp := m.SubmitResponse()
	// Fallback should pick a real option, not commit an empty Other text.
	if resp.Answers[0].Text != "" {
		t.Fatalf("answer.Text = %q, want empty (no empty Other)", resp.Answers[0].Text)
	}
	if len(resp.Answers[0].Selected) != 1 || resp.Answers[0].Selected[0].Label != "First" {
		t.Fatalf("answer.Selected = %v, want [First]", resp.Answers[0].Selected)
	}
}

// TestAskPromptSingleSelectOtherIsMutexWithRealOptions guards against a
// regression where choosing Other on a single-select question (via toggle,
// then Esc out of the text input without finalizing via Enter) left the
// previously picked real option selected, so the answer carried both a
// Selected entry and a Text entry — a contradiction for single-select.
func TestAskPromptSingleSelectOtherIsMutexWithRealOptions(t *testing.T) {
	req := agent.AskRequest{Questions: []agent.AskQuestion{
		{Header: "Q1", Question: "q1?", Options: []agent.AskOption{{Label: "First"}, {Label: "Second"}}},
		{Header: "Q2", Question: "q2?", Options: []agent.AskOption{{Label: "A"}, {Label: "B"}}},
	}}
	m := newAskPromptModel(req)
	// Q1: pick a real option, then choose Other and type a draft, but leave
	// the text input via Esc (ExitOtherMode) instead of Enter.
	m.HandleKey(askType("1"))
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleKey(askKey(tea.KeySpace))
	m.HandleKey(askType("custom"))
	m.ExitOtherMode()
	// Move on and submit without revisiting Q1.
	m.HandleKey(askKey(tea.KeyRight))
	m.HandleKey(askKey(tea.KeyDown))
	m.HandleEnter()
	resp := m.response(false)
	if len(resp.Answers[0].Selected) != 0 {
		t.Fatalf("single-select Other must clear real selection, got %v", resp.Answers[0].Selected)
	}
	if got := strings.TrimSpace(resp.Answers[0].Text); got != "custom" {
		t.Fatalf("Text = %q, want custom", got)
	}
}

// TestAskPromptOtherDraftMultilineSinglePseudoCursor ensures the inline
// Other input renders exactly one trailing pseudo-cursor even when the draft
// wraps across multiple lines.
func TestAskPromptOtherDraftMultilineSinglePseudoCursor(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	m.option = 2
	m.HandleKey(askKey(tea.KeySpace))
	m.HandleKey(askType("the quick brown fox jumps over the lazy dog again and again"))
	view := m.View(30)
	if n := strings.Count(view, "_"); n != 1 {
		t.Fatalf("pseudo-cursor count = %d, want 1\n%s", n, view)
	}
}

// TestAskPromptOtherModeDoesNotDuplicateDraft ensures the in-progress Other
// draft is shown only on the input line while typing, not also echoed as the
// Other option's description.
func TestAskPromptOtherModeDoesNotDuplicateDraft(t *testing.T) {
	m := newAskPromptModel(twoQuestionAskRequest())
	m.option = 2
	m.HandleKey(askKey(tea.KeySpace))
	m.HandleKey(askType("preview text here"))
	view := m.View(50)
	if n := strings.Count(view, "preview text here"); n != 1 {
		t.Fatalf("draft shown %d times in otherMode, want 1\n%s", n, view)
	}
}
