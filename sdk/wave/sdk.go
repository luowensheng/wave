package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

// rpcRequest mirrors infra/plugins.rpcRequest.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse mirrors infra/plugins.rpcResponse.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError mirrors infra/plugins.rpcError.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RunHandler serves a HandlerPlugin over stdin/stdout JSON-RPC.
func RunHandler(impl HandlerPlugin) error {
	return serve(map[string]methodFn{
		MethodHandlerCall: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var req Request
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, err
			}
			return impl.Call(ctx, &req)
		},
	}, impl.Close)
}

// RunStorage serves a StoragePlugin over stdin/stdout JSON-RPC.
func RunStorage(impl StoragePlugin) error {
	return serve(map[string]methodFn{
		MethodStorageGet: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			v, found, err := impl.Get(ctx, p.Key)
			if err != nil {
				return nil, err
			}
			return map[string]any{"value": v, "found": found}, nil
		},
		MethodStorageSet: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Key   string `json:"key"`
				Value []byte `json:"value"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.Set(ctx, p.Key, p.Value)
		},
		MethodStorageDelete: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.Delete(ctx, p.Key)
		},
		MethodStorageQuery: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var q Query
			if err := json.Unmarshal(raw, &q); err != nil {
				return nil, err
			}
			return impl.Query(ctx, &q)
		},
		MethodStorageMigrate: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p MigrationPlan
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.Migrate(ctx, &p)
		},
	}, impl.Close)
}

// RunAuth serves an AuthPlugin over stdin/stdout JSON-RPC.
func RunAuth(impl AuthPlugin) error {
	return serve(map[string]methodFn{
		MethodAuthAuthenticate: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var req AuthRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, err
			}
			return impl.Authenticate(ctx, &req)
		},
		MethodAuthRefreshClaims: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Subject string `json:"subject"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return impl.RefreshClaims(ctx, p.Subject)
		},
		MethodAuthLogout: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Subject string `json:"subject"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.Logout(ctx, p.Subject)
		},
	}, impl.Close)
}

// RunSecrets serves a SecretsPlugin over stdin/stdout JSON-RPC.
func RunSecrets(impl SecretsPlugin) error {
	return serve(map[string]methodFn{
		MethodSecretsResolve: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				URI string `json:"uri"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			v, err := impl.Resolve(ctx, p.URI)
			if err != nil {
				return nil, err
			}
			return map[string]any{"value": v}, nil
		},
	}, impl.Close)
}

// RunExporter serves an ExporterPlugin over stdin/stdout JSON-RPC.
func RunExporter(impl ExporterPlugin) error {
	return serve(map[string]methodFn{
		MethodExporterMetrics: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Samples []*MetricSample `json:"samples"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.ExportMetrics(ctx, p.Samples)
		},
		MethodExporterTraces: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Spans []*TraceSpan `json:"spans"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.ExportTraces(ctx, p.Spans)
		},
		MethodExporterLogs: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Records []*LogRecord `json:"records"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			return nil, impl.ExportLogs(ctx, p.Records)
		},
	}, impl.Close)
}

type methodFn func(ctx context.Context, params json.RawMessage) (any, error)

// serve is the shared loop. It reads newline-terminated JSON-RPC frames
// from stdin, dispatches to the matching handler, and writes responses
// to stdout. The "shutdown" notification ends the loop cleanly.
func serve(handlers map[string]methodFn, closeFn func() error) error {
	return serveOn(os.Stdin, os.Stdout, handlers, closeFn)
}

// serveOn is the testable form of serve.
func serveOn(in io.Reader, out io.Writer, handlers map[string]methodFn, closeFn func() error) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var writeMu sync.Mutex
	var pending sync.WaitGroup
	write := func(resp rpcResponse) error {
		buf, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		buf = append(buf, '\n')
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = out.Write(buf)
		return err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = write(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}})
			continue
		}
		if req.Method == "shutdown" {
			break
		}
		fn, ok := handlers[req.Method]
		if !ok {
			if req.ID != nil {
				_ = write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			}
			continue
		}
		// Capture id by value so the goroutine sees a stable copy.
		idVal := req.ID
		params := req.Params
		method := req.Method
		pending.Add(1)
		go func() {
			defer pending.Done()
			ctx := context.Background()
			defer func() {
				if r := recover(); r != nil {
					if idVal != nil {
						_ = write(rpcResponse{
							JSONRPC: "2.0",
							ID:      idVal,
							Error: &rpcError{
								Code:    -32603,
								Message: fmt.Sprintf("plugin panic in %s: %v\n%s", method, r, debug.Stack()),
							},
						})
					}
				}
			}()
			result, err := fn(ctx, params)
			if idVal == nil {
				return
			}
			if err != nil {
				_ = write(rpcResponse{JSONRPC: "2.0", ID: idVal, Error: &rpcError{Code: -32000, Message: err.Error()}})
				return
			}
			raw, mErr := json.Marshal(result)
			if mErr != nil {
				_ = write(rpcResponse{JSONRPC: "2.0", ID: idVal, Error: &rpcError{Code: -32603, Message: "marshal result: " + mErr.Error()}})
				return
			}
			_ = write(rpcResponse{JSONRPC: "2.0", ID: idVal, Result: raw})
		}()
	}
	pending.Wait()
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if closeFn != nil {
		return closeFn()
	}
	return nil
}
