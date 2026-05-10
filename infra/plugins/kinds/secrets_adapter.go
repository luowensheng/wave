package kinds

import "context"

// secretsAdapter turns the JSON-RPC RPCClient into a typed SecretsPlugin.
type secretsAdapter struct {
	rpc  RPCClient
	name string
}

func (s *secretsAdapter) Resolve(ctx context.Context, uri string) ([]byte, error) {
	var out struct {
		Value []byte `json:"value"`
	}
	if err := rpcCall(ctx, s.rpc, MethodSecretsResolve,
		map[string]any{"uri": uri}, &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

func (s *secretsAdapter) Close() error { return s.rpc.Close() }
