package plugins

import (
	"context"
	"encoding/json"
	"fmt"
)

// rpcRequest is one outbound JSON-RPC 2.0 request. Notifications omit ID.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is one inbound JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// RPCClient is implemented by transports that speak long-lived JSON-RPC.
// It extends the basic Client contract with a typed RPC entry-point
// adapters in infra/plugins/kinds use to call typed plugin methods.
type RPCClient interface {
	Client
	RPC(ctx context.Context, method string, params any) (json.RawMessage, error)
}
