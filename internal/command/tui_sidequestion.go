package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/tui"
)

const sideQuestionSystemPrompt = `<side_question>
You are answering an in-memory BTW side chat inside ub.
- Answer only the BTW side-chat turn. Do not take over or continue the main task.
- No tools are available in this request. Do not request tools, read files, execute commands, search, or inspect external state.
- Never emit native tool calls, tool-call JSON, XML tool tags, <tool_use>, <function=...>, or similar markup.
- Use only the provided main conversation context, BTW side-chat history, and your general knowledge.
- If the question requires fresh workspace inspection, command output, web search, or other tools, say that it needs to be asked as a normal user turn.
- Keep the answer concise and directly useful.
</side_question>`

const sideQuestionToolAttemptMessage = "btw cannot use tools in side chat; ask this as a normal user turn if workspace inspection or commands are needed"

func (r *tuiAgentRunner) AnswerSideQuestion(ctx context.Context, req tui.SideQuestionRequest, events chan<- tui.Event) error {
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return fmt.Errorf("btw question cannot be empty")
	}
	if r == nil || r.provider == nil {
		return fmt.Errorf("btw provider is unavailable")
	}
	workspace := ""
	if r.tools != nil {
		workspace = r.tools.Workspace
	}
	memoryMaxChars := 0
	if r.cfg != nil {
		memoryMaxChars = r.cfg.Memory.MaxChars
	}
	messages := agent.NoToolRuntimeContextMessages(agentRuntimeContext(workspace), workspace, memoryMaxChars)
	messages = append(messages, message.Text(message.RoleSystem, sideQuestionSystemPrompt))
	messages = append(messages, r.sideQuestionHistory()...)
	messages = append(messages, sideQuestionHistoryMessages(req.History)...)
	messages = append(
		messages,
		message.Text(message.RoleUser, question),
	)
	stream, err := r.provider.Chat(ctx, provider.Request{
		Model:     r.model,
		Messages:  messages,
		Tools:     nil,
		Reasoning: cloneReasoningConfig(r.reasoning),
	})
	if err != nil {
		return fmt.Errorf("btw provider %q chat: %w", r.providerName, err)
	}
	consumeErr := consumeSideQuestionStream(ctx, stream, events)
	closeErr := stream.Close()
	if consumeErr != nil {
		return consumeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (r *tuiAgentRunner) sideQuestionHistory() []message.Message {
	if r == nil || r.state == nil {
		return nil
	}
	return sideQuestionTextHistory(r.state.history)
}

func sideQuestionTextHistory(history []message.Message) []message.Message {
	var out []message.Message
	for _, msg := range history {
		switch msg.Role {
		case message.RoleUser, message.RoleAssistant:
			text := strings.TrimSpace(msg.Text())
			if text == "" {
				continue
			}
			out = append(out, message.Text(msg.Role, text))
		}
	}
	return out
}

func sideQuestionHistoryMessages(history []tui.SideQuestionMessage) []message.Message {
	var out []message.Message
	for _, item := range history {
		question := strings.TrimSpace(item.Question)
		answer := strings.TrimSpace(item.Answer)
		if question == "" || answer == "" {
			continue
		}
		out = append(
			out,
			message.Text(message.RoleUser, question),
			message.Text(message.RoleAssistant, item.Answer),
		)
	}
	return out
}

func consumeSideQuestionStream(ctx context.Context, stream provider.Stream, events chan<- tui.Event) error {
	var text strings.Builder
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch event.Type {
		case provider.EventTextDelta:
			nextText := text.String() + event.Text
			if sideQuestionLooksLikeToolMarkup(nextText) {
				return errors.New(sideQuestionToolAttemptMessage)
			}
			text.WriteString(event.Text)
			sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDeltaText, Text: event.Text})
		case provider.EventReasoningDelta, provider.EventUsage:
			continue
		case provider.EventDone:
			if strings.TrimSpace(text.String()) == "" {
				return fmt.Errorf("btw response was empty")
			}
			sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone, Text: text.String()})
			return nil
		case provider.EventToolCall:
			name := strings.TrimSpace(event.ToolName)
			if name == "" {
				name = "tool"
			}
			return fmt.Errorf("%s: %s", sideQuestionToolAttemptMessage, name)
		case provider.EventError:
			if event.Err != nil {
				return event.Err
			}
			return fmt.Errorf("btw provider returned an error event")
		default:
			return fmt.Errorf("btw provider returned unsupported event type %q", event.Type)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return fmt.Errorf("btw response was empty")
	}
	sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone, Text: text.String()})
	return nil
}

func sideQuestionLooksLikeToolMarkup(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"<tool_use",
		"</tool_use",
		"<function=",
		"</function>",
		"<invoke",
		"</invoke>",
		"<tool_name>",
		"</tool_name>",
		"<tool>",
		"</tool>",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, name := range []string{"ls", "read", "glob", "grep", "bash", "edit", "write", "task"} {
		if strings.Contains(lower, "<name>"+name+"</name>") {
			return true
		}
		if strings.Contains(lower, "<"+name) && strings.Contains(lower, "</"+name+">") {
			return true
		}
	}
	return false
}
