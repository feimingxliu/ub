package agent

import (
	"context"

	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

// drainInjected reads any user guidance text from the inject channel and
// appends each as a user message to both the transcript and context message
// slices. It is called between tool-loop iterations so the model sees the
// guidance before the next provider call. The channel is non-blocking: if no
// text is pending, the slices are returned unchanged.
func (a *Agent) drainInjected(ctx context.Context, req Request, contextMessages, transcriptMessages []message.Message) ([]message.Message, []message.Message) {
	if a.inject == nil {
		return contextMessages, transcriptMessages
	}
	for {
		msg, ok := a.readInject(ctx, req)
		if !ok {
			return contextMessages, transcriptMessages
		}
		transcriptMessages = append(transcriptMessages, msg)
		contextMessages = append(contextMessages, msg)
	}
}

// flushRemainingInjected drains any inject guidance the loop never consumed
// (e.g. because the run ended without another tool-call iteration, hit the
// turn limit, or errored out) and persists it to the rollout. It returns the
// messages so callers can fold them into the Result, keeping the runner's
// in-memory history consistent with the rollout. writeCtx is used for the
// append so a cancelled run can still flush — pass context.Background() from
// a defer on the error/interrupt path.
func (a *Agent) flushRemainingInjected(writeCtx context.Context, req Request) []message.Message {
	if a.inject == nil {
		return nil
	}
	var flushed []message.Message
	for {
		msg, ok := a.readInject(writeCtx, req)
		if !ok {
			return flushed
		}
		flushed = append(flushed, msg)
	}
}

// readInject pulls one pending inject text off the channel, persists it as a
// user_message event (reusing the current turn — inject is a mid-turn
// supplement, not a new turn), and returns the resulting message. It returns
// ok=false when the channel is empty or closed.
func (a *Agent) readInject(ctx context.Context, req Request) (message.Message, bool) {
	select {
	case text, ok := <-a.inject:
		if !ok {
			return message.Message{}, false
		}
		msg := message.Text(message.RoleUser, text)
		if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
			return rollout.UserMessage(req.SessionID, req.Turn, msg)
		}); err != nil {
			a.emit(Event{Type: EventError, Content: "inject user message: " + err.Error(), IsError: true, Err: err})
		}
		return msg, true
	default:
		return message.Message{}, false
	}
}
