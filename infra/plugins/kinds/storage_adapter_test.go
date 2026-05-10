package kinds

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeRPC is a minimal RPCClient that lets tests assert what the adapter
// shipped on the wire and return canned responses.
type fakeRPC struct {
	last struct {
		method string
		params any
	}
	resp json.RawMessage
	err  error
}

func (f *fakeRPC) RPC(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.last.method = method
	f.last.params = params
	return f.resp, f.err
}
func (f *fakeRPC) Close() error { return nil }

func TestStorageAdapterGet(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`{"value":"aGk=","found":true}`)}
	a := &storageAdapter{rpc: rc}
	v, ok, err := a.Get(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "hi" {
		t.Errorf("got value=%q ok=%v", v, ok)
	}
	if rc.last.method != MethodStorageGet {
		t.Errorf("method = %s", rc.last.method)
	}
}

func TestStorageAdapterSetDelete(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &storageAdapter{rpc: rc}
	if err := a.Set(context.Background(), "k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodStorageSet {
		t.Errorf("set method = %s", rc.last.method)
	}
	if err := a.Delete(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodStorageDelete {
		t.Errorf("delete method = %s", rc.last.method)
	}
}

func TestStorageAdapterQuery(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`{"columns":["a"],"rows":[{"a":1}],"rows_affected":1}`)}
	a := &storageAdapter{rpc: rc}
	r, err := a.Query(context.Background(), &Query{SQL: "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Columns[0] != "a" || r.RowsAffected != 1 {
		t.Errorf("got %+v", r)
	}
}

func TestStorageAdapterMigrate(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &storageAdapter{rpc: rc}
	if err := a.Migrate(context.Background(), &MigrationPlan{Statements: []string{"create table t(x)"}}); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodStorageMigrate {
		t.Errorf("method = %s", rc.last.method)
	}
}

func TestStorageAdapterPropagatesError(t *testing.T) {
	rc := &fakeRPC{err: errors.New("nope")}
	a := &storageAdapter{rpc: rc}
	if _, _, err := a.Get(context.Background(), "k"); err == nil {
		t.Fatal("expected error")
	}
}
