// Package kinds defines the typed Go interfaces each plugin Kind must
// satisfy. Each interface mirrors the shape of its in-tree counterpart
// (e.g. StoragePlugin mirrors infra/sqlite, AuthPlugin mirrors
// oauth.Provider) so a plugin can be a drop-in replacement.
//
// Adapters in this package wrap the long-lived JSON-RPC RPCClient from
// infra/plugins, marshalling typed Go calls into JSON-RPC method calls.
package kinds

import (
	"context"

	"github.com/luowensheng/wave/infra/plugins"
)

// JSON-RPC method names exposed by handler-kind plugins.
const (
	MethodHandlerCall = "handler.call"
)

// Re-exports so adapters/SDK consumers can stay inside the kinds package.
type (
	// Request is the JSON payload a handler-kind plugin receives.
	Request = plugins.Request
	// Response is the JSON payload a handler-kind plugin returns.
	Response = plugins.Response
)

// HandlerPlugin is the typed interface for KindHandler plugins. The shape
// matches the legacy plugins.Client.Call so handler code is portable.
type HandlerPlugin interface {
	Call(ctx context.Context, req *Request) (*Response, error)
	Close() error
}
