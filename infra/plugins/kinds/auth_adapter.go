package kinds

import "context"

// authAdapter turns the JSON-RPC RPCClient into a typed AuthPlugin.
type authAdapter struct {
	rpc  RPCClient
	name string
}

func (a *authAdapter) Authenticate(ctx context.Context, req *AuthRequest) (*AuthResult, error) {
	var out AuthResult
	if err := rpcCall(ctx, a.rpc, MethodAuthAuthenticate, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *authAdapter) RefreshClaims(ctx context.Context, subject string) (*Claims, error) {
	var out Claims
	if err := rpcCall(ctx, a.rpc, MethodAuthRefreshClaims,
		map[string]any{"subject": subject}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *authAdapter) Logout(ctx context.Context, subject string) error {
	return rpcCall(ctx, a.rpc, MethodAuthLogout,
		map[string]any{"subject": subject}, nil)
}

func (a *authAdapter) Close() error { return a.rpc.Close() }
