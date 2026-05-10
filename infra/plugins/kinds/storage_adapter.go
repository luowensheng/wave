package kinds

import "context"

// storageAdapter turns the JSON-RPC RPCClient into a typed StoragePlugin.
type storageAdapter struct {
	rpc  RPCClient
	name string
}

func (s *storageAdapter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var out struct {
		Value []byte `json:"value"`
		Found bool   `json:"found"`
	}
	if err := rpcCall(ctx, s.rpc, MethodStorageGet, map[string]any{"key": key}, &out); err != nil {
		return nil, false, err
	}
	return out.Value, out.Found, nil
}

func (s *storageAdapter) Set(ctx context.Context, key string, value []byte) error {
	return rpcCall(ctx, s.rpc, MethodStorageSet,
		map[string]any{"key": key, "value": value}, nil)
}

func (s *storageAdapter) Delete(ctx context.Context, key string) error {
	return rpcCall(ctx, s.rpc, MethodStorageDelete, map[string]any{"key": key}, nil)
}

func (s *storageAdapter) Query(ctx context.Context, q *Query) (*QueryResult, error) {
	var out QueryResult
	if err := rpcCall(ctx, s.rpc, MethodStorageQuery, q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *storageAdapter) Migrate(ctx context.Context, plan *MigrationPlan) error {
	return rpcCall(ctx, s.rpc, MethodStorageMigrate, plan, nil)
}

func (s *storageAdapter) Close() error { return s.rpc.Close() }
