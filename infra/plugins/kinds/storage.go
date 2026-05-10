package kinds

import "context"

// JSON-RPC method names exposed by storage-kind plugins.
const (
	MethodStorageGet     = "storage.get"
	MethodStorageSet     = "storage.set"
	MethodStorageDelete  = "storage.delete"
	MethodStorageQuery   = "storage.query"
	MethodStorageMigrate = "storage.migrate"
)

// StoragePlugin is the typed interface for KindStorage plugins. It
// mirrors the shape of infra/sqlite at a deliberately small surface so
// alternate backends (Postgres, DynamoDB, etc.) can swap in.
type StoragePlugin interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Query(ctx context.Context, q *Query) (*QueryResult, error)
	Migrate(ctx context.Context, plan *MigrationPlan) error
	Close() error
}

// Query is one SQL/SQL-ish statement plus parameter binding info.
type Query struct {
	SQL    string         `json:"sql"`
	Args   []any          `json:"args,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

// QueryResult is the canonical return shape for a Query call. Drivers
// that don't naturally produce row maps must convert before returning.
type QueryResult struct {
	Columns      []string         `json:"columns"`
	Rows         []map[string]any `json:"rows"`
	LastInsertID int64            `json:"last_insert_id,omitempty"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
}

// MigrationPlan is an ordered list of DDL statements applied serially.
type MigrationPlan struct {
	Statements []string `json:"statements"`
}
