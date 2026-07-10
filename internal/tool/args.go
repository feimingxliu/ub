package tool

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// IntArg is an integer tool argument that tolerates model-emitted numeric
// strings while still advertising an integer JSON schema.
type IntArg int

func (IntArg) JSONSchemaAlias() any {
	return int(0)
}

func (a *IntArg) UnmarshalJSON(raw []byte) error {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		*a = IntArg(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("expected integer or integer string")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*a = 0
		return nil
	}
	parsed, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("expected integer or integer string")
	}
	*a = IntArg(parsed)
	return nil
}

// BoolArg is a boolean tool argument that tolerates "true"/"false" strings
// while still advertising a boolean JSON schema.
type BoolArg bool

func (BoolArg) JSONSchemaAlias() any {
	return bool(false)
}

func (a *BoolArg) UnmarshalJSON(raw []byte) error {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		*a = BoolArg(b)
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1":
			*a = true
			return nil
		case "false", "0", "":
			*a = false
			return nil
		default:
			return fmt.Errorf("expected boolean or boolean string")
		}
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		switch n {
		case 1:
			*a = true
			return nil
		case 0:
			*a = false
			return nil
		}
	}
	return fmt.Errorf("expected boolean or boolean string")
}
