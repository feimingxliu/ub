package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
)

// askSchemaHint is appended to ask tool error messages so that models
// (especially weaker ones) can self-correct on retry.
const askSchemaHint = `Expected format:
{
  "questions": [{
    "header": "Short section header",
    "question": "The concrete question to ask the user",
    "options": [
      {"label": "Option A", "description": "One sentence explaining this option"},
      {"label": "Option B", "description": "One sentence explaining that option"}
    ],
    "multi_select": false
  }]
}
Each question requires header, question, and options (array of {label, description?} objects, NOT strings). At most 3 questions.`

func askInvalidArgsError(err error) error {
	return fmt.Errorf("ask: invalid args: %w; %s", err, askSchemaHint)
}

func askValidationError(err error) error {
	return fmt.Errorf("%s; %s", err.Error(), askSchemaHint)
}

type askContextKey struct{}

// AskOption is one user-selectable answer for a structured question.
type AskOption struct {
	Label       string `json:"label" jsonschema:"required,description=Short option label shown to the user."`
	Description string `json:"description,omitempty" jsonschema:"description=One sentence explaining the option or tradeoff."`
}

// AskQuestion is one structured preference question from the model.
type AskQuestion struct {
	Header      string      `json:"header" jsonschema:"required,description=Short section header for the choice."`
	Question    string      `json:"question" jsonschema:"required,description=The concrete question to ask the user."`
	Options     []AskOption `json:"options" jsonschema:"required,description=Mutually exclusive options for single-select questions; selectable options for multi-select questions."`
	MultiSelect bool        `json:"multi_select,omitempty" jsonschema:"description=Whether the user may select more than one option."`
}

// AskRequest is the host-facing request used by the ask tool.
type AskRequest struct {
	SessionID string        `json:"session_id,omitempty"`
	UserTurn  int           `json:"user_turn,omitempty"`
	ToolUseID string        `json:"tool_use_id,omitempty"`
	Questions []AskQuestion `json:"questions"`
}

// AskAnswer records the user's choice for one question.
type AskAnswer struct {
	Header   string      `json:"header,omitempty"`
	Question string      `json:"question,omitempty"`
	Selected []AskOption `json:"selected,omitempty"`
	// Text holds a free-form "Other" answer the user typed instead of
	// picking a modeled option. When non-empty it takes precedence over
	// Selected and is what the model sees as the answer.
	Text    string `json:"text,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

// AskResponse is returned by the host UI after the user answers or skips.
type AskResponse struct {
	Answers []AskAnswer `json:"answers,omitempty"`
	Skipped bool        `json:"skipped,omitempty"`
}

// Asker asks the user for structured preferences. It is separate from
// permission.Asker: this is product/implementation direction, not tool
// execution approval.
type Asker interface {
	AskUser(ctx context.Context, req AskRequest) (AskResponse, error)
}

func contextWithAsker(ctx context.Context, asker Asker) context.Context {
	if asker == nil {
		return ctx
	}
	return context.WithValue(ctx, askContextKey{}, asker)
}

func askerFromContext(ctx context.Context) Asker {
	if ctx == nil {
		return nil
	}
	asker, _ := ctx.Value(askContextKey{}).(Asker)
	return asker
}

// NewAskTool returns the structured user-preference ask tool.
func NewAskTool() tool.Tool {
	return &askTool{schema: jsonschema.Reflect(&askArgs{})}
}

type askArgs struct {
	Questions []AskQuestion `json:"questions" jsonschema:"required,description=One to three concrete questions with labeled options."`
}

type askTool struct {
	schema *jsonschema.Schema
}

func (t *askTool) Name() string { return "ask" }

func (t *askTool) Description() string {
	return "Ask the user one or more structured preference questions when the task has a real branch and guessing would cause likely rework. Use sparingly; choose a reasonable default yourself when context is sufficient. In non-interactive runs, the tool returns guidance to proceed with assumptions."
}

func (t *askTool) Schema() *jsonschema.Schema { return t.schema }

func (t *askTool) Risk() tool.Risk { return tool.RiskSafe }

func (t *askTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var args askArgs
	if err := tool.UnmarshalArgs(raw, &args); err != nil {
		return tool.Result{}, askInvalidArgsError(err)
	}
	if err := validateAskQuestions(args.Questions); err != nil {
		return tool.Result{}, askValidationError(err)
	}
	asker := askerFromContext(ctx)
	if asker == nil {
		return tool.Result{
			Content: "No interactive ask UI is available for this run. Proceed with your best judgment, choose a reasonable default, and state your assumptions to the user.",
			Metadata: map[string]string{
				"ask_status": "unavailable",
			},
		}, nil
	}
	resp, err := asker.AskUser(ctx, AskRequest{
		SessionID: tool.SessionIDFromContext(ctx),
		UserTurn:  tool.AgentTurnFromContext(ctx),
		ToolUseID: tool.ToolUseIDFromContext(ctx),
		Questions: cloneAskQuestions(args.Questions),
	})
	if err != nil {
		return tool.Result{}, fmt.Errorf("ask: %w", err)
	}
	return tool.Result{
		Content:  formatAskResponse(resp, args.Questions),
		Metadata: map[string]string{"ask_status": askStatus(resp)},
	}, nil
}

func validateAskQuestions(questions []AskQuestion) error {
	if len(questions) == 0 {
		return fmt.Errorf("ask: questions is required")
	}
	if len(questions) > 3 {
		return fmt.Errorf("ask: at most 3 questions are supported")
	}
	for i, q := range questions {
		if strings.TrimSpace(q.Header) == "" {
			return fmt.Errorf("ask: questions[%d].header is required", i)
		}
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("ask: questions[%d].question is required", i)
		}
		if len(q.Options) == 0 {
			return fmt.Errorf("ask: questions[%d].options is required", i)
		}
		for j, opt := range q.Options {
			if strings.TrimSpace(opt.Label) == "" {
				return fmt.Errorf("ask: questions[%d].options[%d].label is required", i, j)
			}
		}
	}
	return nil
}

func formatAskResponse(resp AskResponse, questions []AskQuestion) string {
	if resp.Skipped {
		return "ask skipped by user; proceed only if you can choose a safe default, and state the assumption."
	}
	answers := resp.Answers
	if len(answers) == 0 {
		answers = defaultAskAnswers(questions)
	}
	var b strings.Builder
	b.WriteString("ask answered:")
	for i, answer := range answers {
		header := strings.TrimSpace(answer.Header)
		if header == "" && i < len(questions) {
			header = questions[i].Header
		}
		question := strings.TrimSpace(answer.Question)
		if question == "" && i < len(questions) {
			question = questions[i].Question
		}
		b.WriteString("\n")
		if header != "" {
			b.WriteString("- ")
			b.WriteString(header)
			b.WriteString(": ")
		} else {
			b.WriteString("- ")
		}
		if answer.Skipped {
			b.WriteString("skipped")
			continue
		}
		if text := strings.TrimSpace(answer.Text); text != "" {
			b.WriteString(text)
			if question != "" {
				b.WriteString(" (")
				b.WriteString(question)
				b.WriteString(")")
			}
			continue
		}
		var labels []string
		for _, selected := range answer.Selected {
			label := strings.TrimSpace(selected.Label)
			if label != "" {
				labels = append(labels, label)
			}
		}
		if len(labels) == 0 {
			b.WriteString("no option selected")
		} else {
			b.WriteString(strings.Join(labels, ", "))
		}
		if question != "" {
			b.WriteString(" (")
			b.WriteString(question)
			b.WriteString(")")
		}
	}
	return b.String()
}

func defaultAskAnswers(questions []AskQuestion) []AskAnswer {
	answers := make([]AskAnswer, 0, len(questions))
	for _, q := range questions {
		answers = append(answers, AskAnswer{
			Header:   q.Header,
			Question: q.Question,
			Skipped:  true,
		})
	}
	return answers
}

func askStatus(resp AskResponse) string {
	if resp.Skipped {
		return "skipped"
	}
	return "answered"
}

func cloneAskQuestions(in []AskQuestion) []AskQuestion {
	out := make([]AskQuestion, len(in))
	for i, q := range in {
		out[i] = q
		out[i].Options = append([]AskOption(nil), q.Options...)
	}
	return out
}

func (a *Agent) recordAskActivity(ctx context.Context, sessionID string, turn int, call toolCall, status, content string, isError bool) {
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityAsk,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       strings.TrimSpace(status),
		Summary:      truncateActivitySummary(askActivitySummary(status, content)),
		Content:      truncateActivityDetail(content),
		IsError:      isError,
	}
	a.emit(event)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Activity(sessionID, turn, rolloutActivityPayload(event))
	}); err != nil {
		a.emit(Event{
			Type:    EventError,
			Content: fmt.Sprintf("record ask activity: %v", err),
			IsError: true,
			Err:     err,
		})
	}
}

func askActivityRequestContent(raw json.RawMessage) string {
	body, ok := decodeToolInput(raw)
	if !ok {
		return "ask request: invalid JSON"
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "ask request: " + string(raw)
	}
	return "ask request:\n" + string(encoded)
}

func askActivitySummary(status, content string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "updated"
	}
	switch status {
	case "requested":
		return "Ask requested"
	case "answered":
		return "Ask answered"
	case "skipped":
		return "Ask skipped"
	case "unavailable":
		return "Ask unavailable"
	case "failed":
		return "Ask failed"
	default:
		if strings.TrimSpace(content) != "" {
			return "Ask " + status + ": " + firstLine(content)
		}
		return "Ask " + status
	}
}
