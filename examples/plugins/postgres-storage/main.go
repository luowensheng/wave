// Command postgres-storage is the reference KindStorage plugin for the
// wave SDK. It exposes a Postgres database over the JSON-RPC
// storage methods (storage.get/set/delete/query/migrate) using pgx/v5.
//
// Configuration is taken from the environment:
//
//	PG_DSN          required — postgres connection string
//	PG_MAX_CONNS    optional — pool size (default 10)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	sdk "wave.dev/sdk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kvDDL is applied lazily on first Get/Set/Delete; idempotent.
const kvDDL = `CREATE TABLE IF NOT EXISTS wave_kv (
	k TEXT PRIMARY KEY,
	v BYTEA NOT NULL
)`

// pgStorage implements sdk.StoragePlugin against a Postgres pool.
type pgStorage struct {
	pool *pgxpool.Pool

	kvOnce sync.Once
	kvErr  error

	log *slog.Logger
}

func newPgStorage(ctx context.Context, log *slog.Logger) (*pgStorage, error) {
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		return nil, errors.New("PG_DSN environment variable is required")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PG_DSN: %w", err)
	}
	if v := os.Getenv("PG_MAX_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("PG_MAX_CONNS=%q invalid", v)
		}
		cfg.MaxConns = int32(n)
	} else {
		cfg.MaxConns = 10
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(dialCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &pgStorage{pool: pool, log: log}, nil
}

// errNoPool is returned by every method when the pool failed to come up
// at boot. The plugin process stays alive so the orchestrator can
// receive a structured error rather than an unexpected exit.
var errNoPool = errors.New("postgres-storage: connection pool not initialized")

// ensureKV lazily creates the key-value table used by Get/Set/Delete.
func (p *pgStorage) ensureKV(ctx context.Context) error {
	if p.pool == nil {
		return errNoPool
	}
	p.kvOnce.Do(func() {
		_, p.kvErr = p.pool.Exec(ctx, kvDDL)
	})
	return p.kvErr
}

func (p *pgStorage) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := p.ensureKV(ctx); err != nil {
		return nil, false, err
	}
	var v []byte
	err := p.pool.QueryRow(ctx, `SELECT v FROM wave_kv WHERE k = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (p *pgStorage) Set(ctx context.Context, key string, value []byte) error {
	if err := p.ensureKV(ctx); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO wave_kv(k, v) VALUES ($1, $2)
		 ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v`, key, value)
	return err
}

func (p *pgStorage) Delete(ctx context.Context, key string) error {
	if err := p.ensureKV(ctx); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM wave_kv WHERE k = $1`, key)
	return err
}

// Query runs q.SQL via pgx and returns column names + JSON-friendly rows.
func (p *pgStorage) Query(ctx context.Context, q *sdk.Query) (*sdk.QueryResult, error) {
	if q == nil || q.SQL == "" {
		return nil, errors.New("empty query")
	}
	if p.pool == nil {
		return nil, errNoPool
	}
	rows, err := p.pool.Query(ctx, q.SQL, q.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}

	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = jsonFriendly(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	res := &sdk.QueryResult{Columns: cols, Rows: out}
	if tag := rows.CommandTag(); tag.RowsAffected() > 0 && len(out) == 0 {
		res.RowsAffected = tag.RowsAffected()
	}
	return res, nil
}

// Migrate applies the plan inside a single transaction. Either every
// statement succeeds or none do.
func (p *pgStorage) Migrate(ctx context.Context, plan *sdk.MigrationPlan) error {
	if plan == nil || len(plan.Statements) == 0 {
		return nil
	}
	if p.pool == nil {
		return errNoPool
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, s := range plan.Statements {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("statement %q: %w", s, err)
		}
	}
	return tx.Commit(ctx)
}

func (p *pgStorage) Close() error {
	if p.pool != nil {
		p.pool.Close()
	}
	return nil
}

// jsonFriendly normalises pgx values into Go types that round-trip
// cleanly through encoding/json (the JSON-RPC wire format).
func jsonFriendly(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		// pgx returns most scalars as native Go types already
		// (int64, float64, string, bool, nil). Composite types fall
		// through to encoding/json's default handling.
		return x
	}
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	store, err := newPgStorage(context.Background(), log)
	if err != nil {
		// Don't exit — let the JSON-RPC loop run so the orchestrator gets
		// a structured error per-call instead of the process disappearing.
		log.Error("postgres-storage init failed", "err", err)
		store = &pgStorage{log: log}
	}
	if err := sdk.RunStorage(store); err != nil {
		log.Error("postgres-storage exited with error", "err", err)
		os.Exit(1)
	}
}
