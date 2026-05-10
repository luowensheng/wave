// Command vault-secrets is the reference secrets-kind plugin. It
// resolves URIs against a HashiCorp Vault KV-v2 HTTP API using only
// the standard library — no Vault SDK dependency.
//
// URI format: "<kvpath>#<jsonkey>"
//   - kvpath  : Vault API path under /v1/, e.g. "secret/data/db"
//   - jsonkey : dotted path inside the KV-v2 ".data.data" object,
//               e.g. "password" or "creds.token"
//
// Required env vars:
//   VAULT_ADDR        e.g. http://vault:8200
//   VAULT_TOKEN       Vault auth token
// Optional:
//   VAULT_CACHE_TTL   duration string, default "5m"
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	sdk "wave.dev/sdk"
)

type vaultPlugin struct {
	addr     string
	token    string
	client   *http.Client
	cacheTTL time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value   []byte
	expires time.Time
}

func (p *vaultPlugin) Resolve(ctx context.Context, uri string) ([]byte, error) {
	if v, ok := p.cacheGet(uri); ok {
		return v, nil
	}
	kvpath, jsonkey, ok := strings.Cut(uri, "#")
	if !ok || kvpath == "" || jsonkey == "" {
		return nil, fmt.Errorf("invalid uri %q: expected kvpath#jsonkey", uri)
	}
	if p.addr == "" {
		return nil, fmt.Errorf("VAULT_ADDR is unset")
	}
	if p.token == "" {
		return nil, fmt.Errorf("VAULT_TOKEN is unset")
	}
	url := strings.TrimRight(p.addr, "/") + "/v1/" + strings.TrimLeft(kvpath, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vault read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault GET %s: status %d: %s", url, resp.StatusCode, truncate(string(body), 256))
	}
	var parsed struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("vault decode: %w", err)
	}
	v, err := lookupDot(parsed.Data.Data, jsonkey)
	if err != nil {
		return nil, err
	}
	out, err := coerce(v)
	if err != nil {
		return nil, err
	}
	p.cachePut(uri, out)
	return out, nil
}

func (p *vaultPlugin) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

func (p *vaultPlugin) cacheGet(uri string) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.cache[uri]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expires) {
		delete(p.cache, uri)
		return nil, false
	}
	return e.value, true
}

func (p *vaultPlugin) cachePut(uri string, v []byte) {
	if p.cacheTTL <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cache == nil {
		p.cache = map[string]cacheEntry{}
	}
	p.cache[uri] = cacheEntry{value: v, expires: time.Now().Add(p.cacheTTL)}
}

// lookupDot walks a dotted path inside a nested map[string]any.
func lookupDot(m map[string]any, path string) (any, error) {
	parts := strings.Split(path, ".")
	var cur any = m
	for i, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("key %q: parent at %q is not an object", path, strings.Join(parts[:i], "."))
		}
		v, ok := mm[p]
		if !ok {
			return nil, fmt.Errorf("key %q not found in vault response", path)
		}
		cur = v
	}
	return cur, nil
}

func coerce(v any) ([]byte, error) {
	switch x := v.(type) {
	case string:
		return []byte(x), nil
	case []byte:
		return x, nil
	case nil:
		return nil, fmt.Errorf("value is null")
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("encode value: %w", err)
		}
		return b, nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func newPlugin() *vaultPlugin {
	ttl := 5 * time.Minute
	if v := os.Getenv("VAULT_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	return &vaultPlugin{
		addr:     os.Getenv("VAULT_ADDR"),
		token:    os.Getenv("VAULT_TOKEN"),
		client:   &http.Client{Timeout: 10 * time.Second},
		cacheTTL: ttl,
	}
}

func main() {
	if err := sdk.RunSecrets(newPlugin()); err != nil {
		fmt.Fprintln(os.Stderr, "vault-secrets:", err)
		os.Exit(1)
	}
}
