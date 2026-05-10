package plugins

import "context"

// gRPC and WASM transports are recognized in config but not implemented
// in this build. They keep the registry honest (config validation passes)
// while returning a clear error at call time so users discover the gap
// immediately and can swap transport=process or transport=http.

type grpcStub struct{ cfg *PluginConfig }
type wasmStub struct{ cfg *PluginConfig }

func newGRPCStub(cfg *PluginConfig) Client { return &grpcStub{cfg: cfg} }
func newWASMStub(cfg *PluginConfig) Client { return &wasmStub{cfg: cfg} }

func (g *grpcStub) Call(ctx context.Context, req *Request) (*Response, error) {
	return nil, ErrNotImplemented
}
func (g *grpcStub) Close() error { return nil }

func (w *wasmStub) Call(ctx context.Context, req *Request) (*Response, error) {
	return nil, ErrNotImplemented
}
func (w *wasmStub) Close() error { return nil }
