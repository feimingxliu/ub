package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
)

// IsServerError reports whether err is a JSON-RPC error returned by the
// remote MCP server (as opposed to a transport-level failure such as a
// broken stdio pipe or an HTTP read error). Server errors describe a
// per-call problem (bad arguments, tool failure) and must not trigger a
// reconnect; transport errors mean the connection itself is unhealthy.
func IsServerError(err error) bool {
	var rpc *rpcError
	return errors.As(err, &rpc)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("mcp: json-rpc error %d", e.Code)
	}
	return fmt.Sprintf("mcp: json-rpc error %d: %s", e.Code, e.Message)
}
