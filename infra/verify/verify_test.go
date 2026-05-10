package verify

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestIssueAndConsume(t *testing.T) {
	iss := NewIssuer(NewMemoryStore(), []byte("k"))
	ctx := context.Background()
	tok, err := iss.Issue(ctx, IssueOpts{Subject: "email-verify", Value: "alice@x.io"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := iss.Consume(ctx, "email-verify", tok)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != "alice@x.io" {
		t.Errorf("value = %q", got)
	}
}

func TestConsumeIsSingleUse(t *testing.T) {
	iss := NewIssuer(NewMemoryStore(), []byte("k"))
	ctx := context.Background()
	tok, _ := iss.Issue(ctx, IssueOpts{Subject: "s", Value: "v"})
	_, err := iss.Consume(ctx, "s", tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Consume(ctx, "s", tok); err == nil {
		t.Error("second consume should fail")
	}
}

func TestSubjectBindingPreventsCrossUse(t *testing.T) {
	iss := NewIssuer(NewMemoryStore(), []byte("k"))
	ctx := context.Background()
	tok, _ := iss.Issue(ctx, IssueOpts{Subject: "magic-link", Value: "u1"})
	if _, err := iss.Consume(ctx, "password-reset", tok); err == nil {
		t.Error("token from one subject should not work for another")
	}
}

func TestExpired(t *testing.T) {
	iss := NewIssuer(NewMemoryStore(), []byte("k"))
	ctx := context.Background()
	tok, _ := iss.Issue(ctx, IssueOpts{Subject: "s", Value: "v", TTL: 10 * time.Millisecond})
	time.Sleep(20 * time.Millisecond)
	if _, err := iss.Consume(ctx, "s", tok); err == nil {
		t.Error("expected expired error")
	}
}

func TestNumericPIN(t *testing.T) {
	iss := NewIssuer(NewMemoryStore(), []byte("k"))
	ctx := context.Background()
	tok, err := iss.Issue(ctx, IssueOpts{Subject: "phone", Value: "+15551234", Numeric: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 6 {
		t.Errorf("PIN length = %d (%q)", len(tok), tok)
	}
	for _, c := range tok {
		if c < '0' || c > '9' {
			t.Errorf("PIN has non-digit: %q", tok)
		}
	}
}

func TestCleanup(t *testing.T) {
	store := NewMemoryStore()
	iss := NewIssuer(store, []byte("k"))
	ctx := context.Background()
	_, _ = iss.Issue(ctx, IssueOpts{Subject: "s", Value: "v1", TTL: 5 * time.Millisecond})
	_, _ = iss.Issue(ctx, IssueOpts{Subject: "s", Value: "v2", TTL: time.Hour})
	time.Sleep(15 * time.Millisecond)
	n, err := iss.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("cleaned %d, want 1", n)
	}
}

func TestSQLiteStore(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	iss := NewIssuer(store, []byte("k"))
	ctx := context.Background()
	tok, _ := iss.Issue(ctx, IssueOpts{Subject: "email-verify", Value: "a@x"})
	got, err := iss.Consume(ctx, "email-verify", tok)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a@x" {
		t.Errorf("got %q", got)
	}
	if _, err := iss.Consume(ctx, "email-verify", tok); err == nil {
		t.Error("re-use should fail")
	}
}
