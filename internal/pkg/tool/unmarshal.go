package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UnmarshalArgs decodes model-emitted tool arguments. Some providers/models
// occasionally wrap the entire argument object as a JSON string; accept that
// form while preserving normal JSON object schemas.
func UnmarshalArgs(raw json.RawMessage, dst any) error {
	if err := json.Unmarshal(raw, dst); err == nil {
		return nil
	} else {
		var encoded string
		if stringErr := json.Unmarshal(raw, &encoded); stringErr != nil {
			return err
		}
		encoded = strings.TrimSpace(encoded)
		if encoded == "" || !(strings.HasPrefix(encoded, "{") || strings.HasPrefix(encoded, "[")) {
			return err
		}
		if retryErr := json.Unmarshal([]byte(encoded), dst); retryErr != nil {
			return fmt.Errorf("%w; decode JSON string args: %v", err, retryErr)
		}
		return nil
	}
}

func DecodeArgs(toolName string, raw json.RawMessage, dst any) error {
	if err := UnmarshalArgs(raw, dst); err != nil {
		return fmt.Errorf("%s: invalid args: %w", toolName, err)
	}
	return nil
}
