// Package graphql is a tiny GraphQL surface over the plugin system.
// It does NOT implement the GraphQL spec — that would dwarf this
// codebase. Instead it accepts the canonical GraphQL POST shape
// ({"query": "...", "variables": {...}, "operationName": "..."}),
// dispatches to a configured plugin with `trigger_key` set to the
// operationName (or "default" if missing), and returns the plugin's
// JSON body verbatim.
//
// The intended workflow:
//   1. Drop your GraphQL resolvers into a plugin (any transport).
//   2. Configure `type: graphql` with `plugin: my_resolver`.
//   3. Frontend POSTs `{"query": "{ users { id } }", ...}` to /graphql.
//   4. Plugin receives `{trigger_key: operationName, body: {query, variables}}`,
//      returns `{data: ...}` or `{errors: [...]}`.
//
// This keeps wave out of the schema-parsing / type-resolution
// business while still giving users an idiomatic GraphQL endpoint.
package graphql

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/luowensheng/wave/infra/plugins"
)

type Config struct {
	Plugin string `yaml:"plugin,omitempty" json:"plugin,omitempty"`
	// IntrospectPlugin (optional) — if set, schema introspection queries
	// are routed to this separate plugin so resolver and introspection
	// can scale independently.
	IntrospectPlugin string `yaml:"introspect_plugin,omitempty" json:"introspect_plugin,omitempty"`
}

type gqlBody struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

func (c *Config) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c == nil || c.Plugin == "" {
		return nil, fmt.Errorf("graphql route requires `plugin`")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := readGraphQLBody(r)
		if err != nil {
			gqlError(w, http.StatusBadRequest, err.Error())
			return
		}
		raw, _ := json.Marshal(body)

		reg := plugins.Default()
		if reg == nil {
			gqlError(w, http.StatusInternalServerError, "plugin registry not initialized")
			return
		}
		pluginName := c.Plugin
		if c.IntrospectPlugin != "" && isIntrospection(body.Query) {
			pluginName = c.IntrospectPlugin
		}
		client, ok := reg.Get(pluginName)
		if !ok {
			gqlError(w, http.StatusInternalServerError, "plugin not found: "+pluginName)
			return
		}

		trigger := body.OperationName
		if trigger == "" {
			trigger = "default"
		}

		resp, err := client.Call(r.Context(), &plugins.Request{
			TriggerKey: trigger,
			Body:       raw,
			Metadata: map[string]string{
				"route_path": r.URL.Path,
				"method":     r.Method,
			},
		})
		if err != nil {
			gqlError(w, http.StatusBadGateway, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		status := resp.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if len(resp.Body) > 0 {
			_, _ = w.Write(resp.Body)
		}
	}, nil
}

// readGraphQLBody supports POST {query, variables, operationName} as
// well as GET ?query=...&operationName=... per the GraphQL HTTP spec.
func readGraphQLBody(r *http.Request) (gqlBody, error) {
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		body := gqlBody{
			Query:         q.Get("query"),
			OperationName: q.Get("operationName"),
		}
		if v := q.Get("variables"); v != "" {
			_ = json.Unmarshal([]byte(v), &body.Variables)
		}
		if body.Query == "" {
			return body, fmt.Errorf("missing `query` parameter")
		}
		return body, nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return gqlBody{}, fmt.Errorf("read body: %w", err)
	}
	defer r.Body.Close()
	var body gqlBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, fmt.Errorf("invalid JSON: %w", err)
	}
	if body.Query == "" {
		return body, fmt.Errorf("missing `query` field")
	}
	return body, nil
}

func gqlError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": msg}},
	})
}

// isIntrospection detects the standard introspection-query prefix
// without parsing GraphQL. False positives cost nothing — they just
// route to the introspect_plugin instead.
func isIntrospection(q string) bool {
	for _, marker := range []string{"__schema", "__type", "IntrospectionQuery"} {
		if contains(q, marker) {
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
