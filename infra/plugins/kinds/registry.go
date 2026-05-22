package kinds

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luowensheng/wave/infra/plugins"
)

// RPCClient is the minimum surface a kind adapter needs: a single
// JSON-RPC entry-point. Re-declared here so the adapters depend on a
// kinds-local symbol — easier to fake in tests, no infra/plugins import
// required from the SDK.
type RPCClient interface {
	RPC(ctx context.Context, method string, params any) (json.RawMessage, error)
	Close() error
}

// LoadHandler returns a HandlerPlugin adapter for every plugin in reg
// whose configured Kind is empty or KindHandler.
func LoadHandler(reg *plugins.Registry) map[string]HandlerPlugin {
	out := map[string]HandlerPlugin{}
	walkRegistry(reg, plugins.KindHandler, func(name string, c plugins.Client) {
		out[name] = handlerAdapter{name: name, client: c}
	})
	return out
}

// LoadStorage returns a StoragePlugin adapter for every storage-kind
// plugin in reg.
func LoadStorage(reg *plugins.Registry) map[string]StoragePlugin {
	out := map[string]StoragePlugin{}
	walkRegistry(reg, plugins.KindStorage, func(name string, c plugins.Client) {
		if rc, ok := asRPC(c); ok {
			out[name] = &storageAdapter{rpc: rc, name: name}
		}
	})
	return out
}

// LoadAuth returns an AuthPlugin adapter for every auth-kind plugin.
func LoadAuth(reg *plugins.Registry) map[string]AuthPlugin {
	out := map[string]AuthPlugin{}
	walkRegistry(reg, plugins.KindAuth, func(name string, c plugins.Client) {
		if rc, ok := asRPC(c); ok {
			out[name] = &authAdapter{rpc: rc, name: name}
		}
	})
	return out
}

// LoadSecrets returns a SecretsPlugin adapter for every secrets-kind plugin.
func LoadSecrets(reg *plugins.Registry) map[string]SecretsPlugin {
	out := map[string]SecretsPlugin{}
	walkRegistry(reg, plugins.KindSecrets, func(name string, c plugins.Client) {
		if rc, ok := asRPC(c); ok {
			out[name] = &secretsAdapter{rpc: rc, name: name}
		}
	})
	return out
}

// LoadExporter returns an ExporterPlugin adapter for every exporter-kind plugin.
func LoadExporter(reg *plugins.Registry) map[string]ExporterPlugin {
	out := map[string]ExporterPlugin{}
	walkRegistry(reg, plugins.KindExporter, func(name string, c plugins.Client) {
		if rc, ok := asRPC(c); ok {
			out[name] = &exporterAdapter{rpc: rc, name: name}
		}
	})
	return out
}

// asRPC narrows a plugins.Client to the RPCClient surface kinds need.
func asRPC(c plugins.Client) (RPCClient, bool) {
	rc, ok := c.(plugins.RPCClient)
	if !ok {
		return nil, false
	}
	return rc, true
}

// walkRegistry exposes the registry contents to typed loaders. The
// global accessor keeps it stable enough for tests; production code
// should call the typed Load* functions instead.
func walkRegistry(reg *plugins.Registry, kind string, fn func(name string, c plugins.Client)) {
	if reg == nil {
		return
	}
	for _, name := range reg.NamesOfKind(kind) {
		c, ok := reg.Get(name)
		if !ok {
			continue
		}
		fn(name, c)
	}
}

// handlerAdapter exposes a plugins.Client through the HandlerPlugin shape.
type handlerAdapter struct {
	name   string
	client plugins.Client
}

func (h handlerAdapter) Call(ctx context.Context, req *Request) (*Response, error) {
	return h.client.Call(ctx, req)
}

func (h handlerAdapter) Close() error { return h.client.Close() }

// rpcCall is a small helper that marshals/unmarshals around RPCClient.RPC.
func rpcCall(ctx context.Context, rc RPCClient, method string, params any, out any) error {
	raw, err := rc.RPC(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}
