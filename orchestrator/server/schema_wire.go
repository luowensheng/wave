package servers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/luowensheng/wave/infra/jsonschema"
)

// schemaMiddleware builds an http middleware that validates the JSON
// request body against the configured schema before invoking next.
// Body is buffered + restored so downstream handlers can re-read it.
func schemaMiddleware(cfg *RouteSchemaConfig) (func(http.Handler) http.Handler, error) {
	if cfg == nil {
		return nil, nil
	}
	var schemaBytes []byte
	if cfg.Inline != nil {
		b, err := json.Marshal(cfg.Inline)
		if err != nil {
			return nil, fmt.Errorf("inline schema: %w", err)
		}
		schemaBytes = b
	} else if cfg.Path != "" {
		b, err := os.ReadFile(cfg.Path)
		if err != nil {
			return nil, fmt.Errorf("schema file: %w", err)
		}
		schemaBytes = b
	} else {
		return nil, nil
	}

	schema, err := jsonschema.Parse(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only validate methods that conventionally have bodies.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodDelete {
				next.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
			if err != nil {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			if len(bytes.TrimSpace(body)) == 0 {
				http.Error(w, "request body required", http.StatusBadRequest)
				return
			}
			var v any
			if err := json.Unmarshal(body, &v); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			if errs := schema.Validate(v); len(errs) > 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":  "schema validation failed",
					"issues": errs,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
