package agent

import (
	"encoding/json"

	"github.com/feimingxliu/ub/internal/message"
)

// cloneMessages returns a deep copy of the message slice. Each message is
// individually cloned so the caller can mutate the returned slice without
// affecting the original. Returns nil for nil input.
func cloneMessages(messages []message.Message) []message.Message {
	if messages == nil {
		return nil
	}
	out := make([]message.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg.Clone()
	}
	return out
}

// cloneRaw returns a copy of a json.RawMessage byte slice. This is necessary
// because RawMessage is a []byte alias and slices share backing arrays — without
// copying, mutations to the original would affect the clone and vice versa.
func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
