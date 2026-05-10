package outbox

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func newStore(t *testing.T) *SQLiteStore {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnqueueClaimDelete(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	if err := store.Enqueue(ctx, &Delivery{
		URL: "http://x", Body: []byte(`{}`),
		NextTryAt: time.Now().Add(-time.Minute), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ClaimDue(ctx, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("claim got %d err=%v", len(got), err)
	}
	if got[0].URL != "http://x" {
		t.Errorf("url = %q", got[0].URL)
	}
	if err := store.Succeed(ctx, got[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.Pending(ctx); n != 0 {
		t.Errorf("pending = %d after succeed", n)
	}
}

func TestClaimDueRespectsNextTryAt(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	// Future delivery → not claimed.
	_ = store.Enqueue(ctx, &Delivery{
		URL: "http://future", NextTryAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
	})
	got, _ := store.ClaimDue(ctx, 10)
	if len(got) != 0 {
		t.Errorf("future delivery claimed: %d", len(got))
	}
}

func TestOutboxDrainsToHTTPUpstream(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("X-Tag") != "1" {
			t.Errorf("missing X-Tag")
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	store := newStore(t)
	ob := New(store, 5, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ob.Start(ctx, 20*time.Millisecond)
	defer ob.Stop()

	if err := ob.Enqueue(ctx, upstream.URL, []byte(`{"x":1}`), map[string]string{"X-Tag": "1"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hits.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d", hits.Load())
	}
	if n, _ := store.Pending(ctx); n != 0 {
		t.Errorf("pending after success = %d", n)
	}
}

func TestOutboxRetriesOn5xx(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n < 3 {
			http.Error(w, "boom", 500)
			return
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	store := newStore(t)
	// Override default backoff via low-attempt manipulation: we use small
	// ticks + the fact that Fail() schedules backoff; jitter aside, the
	// test waits long enough for 3 attempts.
	ob := New(store, 5, &http.Client{Timeout: 2 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ob.Start(ctx, 50*time.Millisecond)
	defer ob.Stop()
	_ = ob.Enqueue(ctx, upstream.URL, []byte(`{}`), nil)

	deadline := time.Now().Add(8 * time.Second) // backoff 1s + 2s = ~3s minimum
	for time.Now().Before(deadline) {
		if s, _, _, _ := ob.Stats(); s == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if s, _, _, _ := ob.Stats(); s != 1 {
		t.Errorf("success counter = %d (hits=%d)", s, hits.Load())
	}
	if hits.Load() < 3 {
		t.Errorf("hits = %d, expected at least 3", hits.Load())
	}
}

func TestOutboxDeadLettersAfterMaxRetries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always fail", 500)
	}))
	defer upstream.Close()

	store := newStore(t)
	ob := New(store, 2, &http.Client{Timeout: 2 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ob.Start(ctx, 30*time.Millisecond)
	defer ob.Stop()
	_ = ob.Enqueue(ctx, upstream.URL, []byte(`{}`), nil)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, f, _, _ := ob.Stats(); f == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, f, _, pending := ob.Stats()
	if f != 1 {
		t.Errorf("failTotal = %d", f)
	}
	if pending != 0 {
		t.Errorf("dead-lettered delivery should be removed; pending=%d", pending)
	}
	// And the DLQ should now contain it for inspection.
	dlq, err := store.DLQList(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dlq) != 1 {
		t.Errorf("expected 1 DLQ entry, got %d", len(dlq))
	}
	if dlq[0].LastError == "" {
		t.Errorf("DLQ entry missing last_error: %+v", dlq[0])
	}
}

func TestHeaderCodec(t *testing.T) {
	in := map[string]string{"Content-Type": "application/json", "X-A": "1=2"}
	enc := encodeHeaders(in)
	out := decodeHeaders(enc)
	if out["Content-Type"] != in["Content-Type"] || out["X-A"] != in["X-A"] {
		t.Errorf("round-trip lost data: %v", out)
	}
}
