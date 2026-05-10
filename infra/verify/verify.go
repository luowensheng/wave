// Package verify is a generic single-use-token store. Three flows
// share the same primitive:
//
//   - email verification: subject="email", value=<address>
//   - magic-link login:   subject="magic-link", value=<user-id>
//   - password reset:     subject="password-reset", value=<user-id>
//   - phone verification: subject="phone", value=<phone>
//
// Tokens are random URL-safe strings (default 32 chars). Issue ties a
// token to (Subject, Value, ExpiresAt). Consume returns the value if
// the token is valid, then deletes it (single-use). PIN style is
// supported via the Numeric option for SMS-friendly 6-digit codes.
package verify

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Token is what gets handed to the user (in a URL or SMS body).
type Token string

// Record is the persisted form.
type Record struct {
	Token     Token
	Subject   string
	Value     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store persists Records.
type Store interface {
	Issue(ctx context.Context, r Record) error
	Consume(ctx context.Context, subject string, t Token) (string, error)
	Cleanup(ctx context.Context) (int, error)
}

// IssueOpts controls one Issue() call.
type IssueOpts struct {
	Subject  string
	Value    string
	TTL      time.Duration // default 1h
	Numeric  bool          // 6-digit PIN instead of random URL-safe string
	Length   int           // override token length (URL-safe)
}

// Issuer is the high-level handle most callers use. It hashes tokens
// before storing them so a database leak doesn't burn live tokens —
// only the user's email/SMS contains the cleartext.
type Issuer struct {
	store Store
	hmacKey []byte
}

// NewIssuer wraps a Store with a HMAC key used to hash tokens at rest.
// The key should be persistent (env var) so server restarts don't
// invalidate live tokens.
func NewIssuer(store Store, hmacKey []byte) *Issuer {
	if len(hmacKey) == 0 {
		// Use a fresh random key — fine for ephemeral dev runs; in prod
		// pass a stable key from secrets.
		hmacKey = make([]byte, 32)
		_, _ = rand.Read(hmacKey)
	}
	return &Issuer{store: store, hmacKey: hmacKey}
}

// Issue generates a fresh token, persists its hash, returns the
// cleartext token to embed in the user-facing URL/SMS.
func (i *Issuer) Issue(ctx context.Context, opts IssueOpts) (Token, error) {
	if opts.Subject == "" || opts.Value == "" {
		return "", fmt.Errorf("verify: subject and value required")
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	tok, err := generate(opts)
	if err != nil {
		return "", err
	}
	hashed := i.hash(opts.Subject, tok)
	r := Record{
		Token: hashed, Subject: opts.Subject, Value: opts.Value,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(ttl),
	}
	if err := i.store.Issue(ctx, r); err != nil {
		return "", err
	}
	return tok, nil
}

// Consume validates + deletes a token. Returns the original value
// (e.g. the verified email or user-id), or an error.
func (i *Issuer) Consume(ctx context.Context, subject string, t Token) (string, error) {
	hashed := i.hash(subject, t)
	return i.store.Consume(ctx, subject, hashed)
}

// Cleanup deletes expired records. Run periodically from cron.
func (i *Issuer) Cleanup(ctx context.Context) (int, error) {
	return i.store.Cleanup(ctx)
}

// hash binds (subject, token) so a token issued under one subject
// can't be reused under another.
func (i *Issuer) hash(subject string, t Token) Token {
	m := hmac.New(sha256.New, i.hmacKey)
	m.Write([]byte(subject))
	m.Write([]byte{0})
	m.Write([]byte(t))
	return Token(hex.EncodeToString(m.Sum(nil)))
}

func generate(opts IssueOpts) (Token, error) {
	if opts.Numeric {
		return generateNumeric(6)
	}
	n := opts.Length
	if n <= 0 {
		n = 32
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return Token(base64.RawURLEncoding.EncodeToString(b)), nil
}

func generateNumeric(digits int) (Token, error) {
	max := 1
	for i := 0; i < digits; i++ {
		max *= 10
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := 0
	for _, c := range b {
		n = (n*256 + int(c)) % max
	}
	return Token(fmt.Sprintf("%0*d", digits, n)), nil
}

// ── Memory store (tests + dev) ──────────────────────────────────────

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]*Record // subject+"|"+token -> record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]*Record{}}
}

func key(subject string, t Token) string { return subject + "|" + string(t) }

func (m *MemoryStore) Issue(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := r
	m.records[key(r.Subject, r.Token)] = &cp
	return nil
}

func (m *MemoryStore) Consume(_ context.Context, subject string, t Token) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key(subject, t)]
	if !ok {
		return "", errors.New("token not found or already used")
	}
	delete(m.records, key(subject, t))
	if time.Now().After(rec.ExpiresAt) {
		return "", errors.New("token expired")
	}
	return rec.Value, nil
}

func (m *MemoryStore) Cleanup(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	n := 0
	for k, r := range m.records {
		if now.After(r.ExpiresAt) {
			delete(m.records, k)
			n++
		}
	}
	return n, nil
}

// ── SQLite store ────────────────────────────────────────────────────

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _wave_verify_tokens (
		token TEXT NOT NULL,
		subject TEXT NOT NULL,
		value TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		PRIMARY KEY (subject, token)
	); CREATE INDEX IF NOT EXISTS _wave_verify_exp ON _wave_verify_tokens(expires_at);
	`); err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Issue(ctx context.Context, r Record) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO _wave_verify_tokens
		(token, subject, value, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		string(r.Token), r.Subject, r.Value, r.CreatedAt.Unix(), r.ExpiresAt.Unix())
	return err
}

func (s *SQLiteStore) Consume(ctx context.Context, subject string, t Token) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRowContext(ctx, `
		SELECT value, expires_at FROM _wave_verify_tokens
		WHERE subject = ? AND token = ?`, subject, string(t))
	var value string
	var exp int64
	if err := row.Scan(&value, &exp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("token not found or already used")
		}
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM _wave_verify_tokens
		WHERE subject = ? AND token = ?`, subject, string(t)); err != nil {
		return "", err
	}
	if time.Now().Unix() > exp {
		_ = tx.Commit()
		return "", errors.New("token expired")
	}
	return value, tx.Commit()
}

func (s *SQLiteStore) Cleanup(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM _wave_verify_tokens WHERE expires_at < ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
