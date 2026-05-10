package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// httpClient POSTs the JSON Request body to the configured address and
// decodes the response body as a JSON Response. Useful for remote plugins
// or quick adaptors backed by an HTTP server.
type httpClient struct {
	cfg    *PluginConfig
	client *http.Client
}

func newHTTPClient(cfg *PluginConfig) Client {
	return &httpClient{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.timeoutDuration()},
	}
}

func (c *httpClient) Close() error { return nil }

func (c *httpClient) Call(ctx context.Context, req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Address, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Env {
		// allow callers to inject custom headers via env_overrides convention:
		if strings.HasPrefix(k, "HEADER_") {
			hreq.Header.Set(strings.TrimPrefix(k, "HEADER_"), v)
		}
	}

	hresp, err := c.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("plugin http call: %w", err)
	}
	defer hresp.Body.Close()

	respBody, err := io.ReadAll(hresp.Body)
	if err != nil {
		return nil, err
	}

	// Two acceptable shapes:
	//   1. The remote returns a full {status, headers, body} envelope.
	//   2. The remote just returns the body; we wrap it.
	var resp Response
	if err := json.Unmarshal(respBody, &resp); err == nil && resp.Status != 0 {
		return &resp, nil
	}
	return &Response{
		Status:  hresp.StatusCode,
		Headers: map[string]string{"Content-Type": hresp.Header.Get("Content-Type")},
		Body:    json.RawMessage(respBody),
	}, nil
}
