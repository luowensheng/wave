//go:build pgintegration
// +build pgintegration

// Integration tests are gated behind the pgintegration build tag and
// require PG_TEST_DSN to point at a usable Postgres database. Run with:
//
//	PG_TEST_DSN=postgres://localhost/test go test -tags=pgintegration ./...
package main

import (
	"context"
	"log/slog"
	"os"
	"testing"

	sdk "wave.dev/sdk"
)

func dsnOrSkip(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping postgres integration test")
	}
	t.Setenv("PG_DSN", dsn)
	return dsn
}

func newStoreT(t *testing.T) *pgStorage {
	t.Helper()
	dsnOrSkip(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	store, err := newPgStorage(context.Background(), log)
	if err != nil {
		t.Fatalf("newPgStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestKVRoundTrip(t *testing.T) {
	store := newStoreT(t)
	ctx := context.Background()
	if err := store.Set(ctx, "hello", []byte("world")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := store.Get(ctx, "hello")
	if err != nil || !ok || string(v) != "world" {
		t.Fatalf("Get: v=%q ok=%v err=%v", v, ok, err)
	}
	if err := store.Delete(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(ctx, "hello"); ok {
		t.Fatal("expected key gone after delete")
	}
}

func TestQuerySelect(t *testing.T) {
	store := newStoreT(t)
	res, err := store.Query(context.Background(), &sdk.Query{SQL: "SELECT 1 AS n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["n"] == nil {
		t.Fatalf("unexpected result: %+v", res)
	}
}
