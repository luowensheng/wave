// Package sdk is the wave plugin authoring SDK. It mirrors the
// kind interfaces in wave/infra/plugins/kinds without importing
// them, so plugin authors don't need wave as a build-time dep.
//
// Use the Run* helpers to expose your implementation over the standard
// JSON-RPC 2.0 stdin/stdout transport wave speaks to long-lived
// plugin processes.
package sdk

import (
	"context"
	"encoding/json"
)

// JSON-RPC method-name constants. Mirror infra/plugins/kinds.
const (
	MethodHandlerCall = "handler.call"

	MethodStorageGet     = "storage.get"
	MethodStorageSet     = "storage.set"
	MethodStorageDelete  = "storage.delete"
	MethodStorageQuery   = "storage.query"
	MethodStorageMigrate = "storage.migrate"

	MethodAuthAuthenticate  = "auth.authenticate"
	MethodAuthRefreshClaims = "auth.refresh_claims"
	MethodAuthLogout        = "auth.logout"

	MethodSecretsResolve = "secrets.resolve"

	MethodExporterMetrics = "exporter.metrics"
	MethodExporterTraces  = "exporter.traces"
	MethodExporterLogs    = "exporter.logs"
)

// Request is the JSON payload a handler-kind plugin receives.
type Request struct {
	TriggerKey string            `json:"trigger_key,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Cookies    map[string]string `json:"cookies,omitempty"`
	Query      map[string]string `json:"query,omitempty"`
	PathParams map[string]string `json:"path_params,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
}

// Response is the JSON payload a handler-kind plugin returns.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// HandlerPlugin is the interface a KindHandler implementation satisfies.
type HandlerPlugin interface {
	Call(ctx context.Context, req *Request) (*Response, error)
	Close() error
}

// Storage types -----------------------------------------------------------

type Query struct {
	SQL    string         `json:"sql"`
	Args   []any          `json:"args,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type QueryResult struct {
	Columns      []string         `json:"columns"`
	Rows         []map[string]any `json:"rows"`
	LastInsertID int64            `json:"last_insert_id,omitempty"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
}

type MigrationPlan struct {
	Statements []string `json:"statements"`
}

type StoragePlugin interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Query(ctx context.Context, q *Query) (*QueryResult, error)
	Migrate(ctx context.Context, plan *MigrationPlan) error
	Close() error
}

// Auth types --------------------------------------------------------------

type AuthRequest struct {
	Method      string            `json:"method"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
}

type AuthResult struct {
	Authenticated bool      `json:"authenticated"`
	Claims        *Claims   `json:"claims,omitempty"`
	Redirect      string    `json:"redirect,omitempty"`
	SetCookies    []*Cookie `json:"set_cookies,omitempty"`
}

type Claims struct {
	Subject       string         `json:"subject"`
	Email         string         `json:"email,omitempty"`
	EmailVerified bool           `json:"email_verified,omitempty"`
	Name          string         `json:"name,omitempty"`
	Roles         []string       `json:"roles,omitempty"`
	Scopes        []string       `json:"scopes,omitempty"`
	Provider      string         `json:"provider,omitempty"`
	Raw           map[string]any `json:"raw,omitempty"`
}

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	MaxAge   int    `json:"max_age,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
	SameSite string `json:"same_site,omitempty"`
}

type AuthPlugin interface {
	Authenticate(ctx context.Context, req *AuthRequest) (*AuthResult, error)
	RefreshClaims(ctx context.Context, subject string) (*Claims, error)
	Logout(ctx context.Context, subject string) error
	Close() error
}

// Secrets types -----------------------------------------------------------

type SecretsPlugin interface {
	Resolve(ctx context.Context, uri string) ([]byte, error)
	Close() error
}

// Exporter types ----------------------------------------------------------

type MetricSample struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp int64             `json:"ts,omitempty"`
}

type TraceSpan struct {
	TraceID       string            `json:"trace_id"`
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id,omitempty"`
	Name          string            `json:"name"`
	StartUnixNano int64             `json:"start_unix_nano"`
	EndUnixNano   int64             `json:"end_unix_nano"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type LogRecord struct {
	Timestamp int64          `json:"ts"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type ExporterPlugin interface {
	ExportMetrics(ctx context.Context, batch []*MetricSample) error
	ExportTraces(ctx context.Context, batch []*TraceSpan) error
	ExportLogs(ctx context.Context, batch []*LogRecord) error
	Close() error
}
