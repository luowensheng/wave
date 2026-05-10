package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestPlugin(t *testing.T, handler http.HandlerFunc, ttl time.Duration) *vaultPlugin {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &vaultPlugin{
		addr:     srv.URL,
		token:    "fake-token",
		client:   srv.Client(),
		cacheTTL: ttl,
	}
}

func TestResolve_Success(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("X-Vault-Token"); got != "fake-token" {
			t.Errorf("token header %q", got)
		}
		if r.URL.Path != "/v1/secret/data/db" {
			t.Errorf("path %q", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"data":{"password":"s3cr3t"}}}`))
	}, time.Minute)
	got, err := p.Resolve(context.Background(), "secret/data/db#password")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "s3cr3t" {
		t.Errorf("got %q", got)
	}
	if calls != 1 {
		t.Errorf("calls %d", calls)
	}
}

func TestResolve_404(t *testing.T) {
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"errors":["not found"]}`))
	}, time.Minute)
	_, err := p.Resolve(context.Background(), "secret/data/missing#k")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolve_MissingJSONKey(t *testing.T) {
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"data":{"otherkey":"v"}}}`))
	}, time.Minute)
	_, err := p.Resolve(context.Background(), "secret/data/db#password")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolve_DottedKey(t *testing.T) {
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"data":{"creds":{"token":"abc"}}}}`))
	}, time.Minute)
	got, err := p.Resolve(context.Background(), "secret/data/db#creds.token")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_CacheHit(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"data":{"data":{"k":"v"}}}`))
	}, time.Minute)
	for i := 0; i < 3; i++ {
		v, err := p.Resolve(context.Background(), "secret/data/db#k")
		if err != nil || string(v) != "v" {
			t.Fatalf("i=%d err=%v v=%q", i, err, v)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 backend call, got %d", calls)
	}
}

func TestResolve_CacheExpiry(t *testing.T) {
	calls := 0
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"data":{"data":{"k":"v"}}}`))
	}, time.Millisecond)
	if _, err := p.Resolve(context.Background(), "secret/data/db#k"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := p.Resolve(context.Background(), "secret/data/db#k"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("expected 2 backend calls, got %d", calls)
	}
}

func TestResolve_BadURI(t *testing.T) {
	p := newTestPlugin(t, func(w http.ResponseWriter, r *http.Request) {}, time.Minute)
	if _, err := p.Resolve(context.Background(), "no-hash"); err == nil {
		t.Error("expected error for missing #")
	}
}

func TestResolve_MissingAddr(t *testing.T) {
	p := &vaultPlugin{token: "t", client: &http.Client{}}
	if _, err := p.Resolve(context.Background(), "k#v"); err == nil {
		t.Error("expected error")
	}
}
