package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// retryClient wraps an inner Client with exponential-backoff retries on
// transient errors. Wraps *after* metrics so retries don't double-count;
// ordering is set in registry.go.
type retryClient struct {
	inner    Client
	max      int
	baseWait time.Duration
}

func wrapWithRetry(inner Client, cfg *PluginConfig) Client {
	if cfg.Retries <= 0 {
		return inner
	}
	base := 50 * time.Millisecond
	if cfg.RetryBackoff != "" {
		if d, err := time.ParseDuration(cfg.RetryBackoff); err == nil && d > 0 {
			base = d
		}
	}
	return &retryClient{inner: inner, max: cfg.Retries, baseWait: base}
}

func (r *retryClient) Close() error { return r.inner.Close() }

func (r *retryClient) Call(ctx context.Context, req *Request) (*Response, error) {
	var lastErr error
	wait := r.baseWait
	for attempt := 0; attempt <= r.max; attempt++ {
		resp, err := r.inner.Call(ctx, req)
		if err == nil {
			// Treat 5xx from the plugin as transient too — gives upstream
			// a chance to recover from a brief blip without bubbling the
			// failure to the client.
			if resp != nil && resp.Status >= 500 && attempt < r.max {
				lastErr = errStatus5xx
			} else {
				return resp, nil
			}
		} else {
			if !isTransient(err) || attempt >= r.max {
				return nil, err
			}
			lastErr = err
		}

		// Sleep with jitter, but stop early if the request context is dead.
		jittered := wait + time.Duration(rand.Int63n(int64(wait/2)+1))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(jittered):
		}
		wait *= 2
		if wait > 2*time.Second {
			wait = 2 * time.Second
		}
	}
	return nil, lastErr
}

// RPC forwards typed JSON-RPC calls to the wrapped client when it
// implements RPCClient. The decorator does not retry RPC traffic — typed
// adapters surface plugin errors directly so callers can distinguish
// transient transport failures from semantic errors.
func (r *retryClient) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if rc, ok := r.inner.(RPCClient); ok {
		return rc.RPC(ctx, method, params)
	}
	return nil, fmt.Errorf("plugin transport does not support RPC")
}

var errStatus5xx = errors.New("plugin returned 5xx")

// isTransient covers the obvious retryable cases. Deliberately
// conservative — better to surface a real bug than swallow it.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := err.Error()
	for _, m := range []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"no such host",
		"temporarily unavailable",
		"plugin exec",          // subprocess failure
		"plugin produced empty", // empty stdout from subprocess
	} {
		if contains(s, m) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
