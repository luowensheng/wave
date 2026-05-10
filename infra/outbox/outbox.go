// Package outbox is a durable retry queue for outbound HTTP deliveries.
//
// Use case: a webhook arrives, we filter and fan out to a connection
// (in-process, lossy on slow subscribers), AND we want to forward the
// raw payload to a downstream system (audit logger, Slack, customer
// webhook). The downstream might be flaky — the outbox persists each
// delivery to SQLite, retries with exponential backoff, and a
// background worker drains it.
//
// Designed to share the existing SQLite storage layer; no new
// dependencies. State table is self-managing (created on Open).
package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Delivery is one queued outbound HTTP POST.
type Delivery struct {
	ID         int64
	URL        string
	Body       []byte
	Headers    map[string]string
	Attempts   int
	NextTryAt  time.Time
	CreatedAt  time.Time
	LastError  string
}

// Store persists Deliveries. SQLite-backed by default; the interface
// keeps it pluggable for tests / Postgres later.
type Store interface {
	Enqueue(ctx context.Context, d *Delivery) error
	ClaimDue(ctx context.Context, n int) ([]*Delivery, error)
	Succeed(ctx context.Context, id int64) error
	Fail(ctx context.Context, id int64, attempts int, nextTry time.Time, lastErr string) error
	Pending(ctx context.Context) (int, error)
	// DeadLetter moves a permanently-failed delivery to the DLQ table
	// and removes it from the live queue. Lets ops inspect / replay.
	DeadLetter(ctx context.Context, id int64, lastErr string) error
	// DLQList returns up to n most recent dead-lettered entries.
	DLQList(ctx context.Context, n int) ([]*Delivery, error)
}

// Outbox wires a Store to a worker that drains it.
type Outbox struct {
	store      Store
	maxRetries int
	httpClient *http.Client

	successTotal atomic.Int64
	failTotal    atomic.Int64
	retryTotal   atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New constructs an Outbox over store. maxRetries is the cap on
// attempts (after which the delivery is dead-lettered = marked
// permanently failed). httpClient may be nil; a 10s default is used.
func New(store Store, maxRetries int, hc *http.Client) *Outbox {
	if maxRetries <= 0 {
		maxRetries = 10
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Outbox{store: store, maxRetries: maxRetries, httpClient: hc}
}

// Enqueue is the synchronous "make sure this delivery is durable"
// entry point. Stream-publish handlers call this instead of the
// fire-and-forget goroutine they used to use.
func (o *Outbox) Enqueue(ctx context.Context, url string, body []byte, headers map[string]string) error {
	return o.store.Enqueue(ctx, &Delivery{
		URL: url, Body: body, Headers: headers,
		NextTryAt: time.Now(), CreatedAt: time.Now(),
	})
}

// Start spins up a worker goroutine that polls the store every `tick`,
// claims due deliveries, and POSTs them. Cancel via Stop() or by
// cancelling the parent context.
func (o *Outbox) Start(ctx context.Context, tick time.Duration) {
	if tick <= 0 {
		tick = time.Second
	}
	ctx, o.cancel = context.WithCancel(ctx)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				o.drain(ctx)
			}
		}
	}()
}

// Stop signals the worker to exit and waits for it.
func (o *Outbox) Stop() {
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()
}

// Stats exposes counters for /metrics + admin.
func (o *Outbox) Stats() (success, fail, retries int64, pending int) {
	pending, _ = o.store.Pending(context.Background())
	return o.successTotal.Load(), o.failTotal.Load(), o.retryTotal.Load(), pending
}

func (o *Outbox) drain(ctx context.Context) {
	deliveries, err := o.store.ClaimDue(ctx, 32)
	if err != nil {
		return
	}
	for _, d := range deliveries {
		o.deliver(ctx, d)
	}
}

func (o *Outbox) deliver(ctx context.Context, d *Delivery) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Body))
	if err != nil {
		o.recordFailure(ctx, d, fmt.Sprintf("build request: %v", err))
		return
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.recordFailure(ctx, d, fmt.Sprintf("post: %v", err))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		o.successTotal.Add(1)
		_ = o.store.Succeed(ctx, d.ID)
		return
	}
	o.recordFailure(ctx, d, fmt.Sprintf("status %d", resp.StatusCode))
}

func (o *Outbox) recordFailure(ctx context.Context, d *Delivery, msg string) {
	d.Attempts++
	if d.Attempts >= o.maxRetries {
		o.failTotal.Add(1)
		// Move to the DLQ so ops can inspect and (later) replay.
		_ = o.store.DeadLetter(ctx, d.ID, msg)
		return
	}
	o.retryTotal.Add(1)
	// Exponential backoff: 1s, 2s, 4s, 8s, ... capped at 5min.
	backoff := time.Duration(1<<min(d.Attempts, 8)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	_ = o.store.Fail(ctx, d.ID, d.Attempts, time.Now().Add(backoff), msg)
}

func min(a, b int) int { if a < b { return a }; return b }

// ── SQLite-backed Store ────────────────────────────────────────────────────

// SQLiteStore is the production Store implementation.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // serializes ClaimDue to keep the example simple
}

// NewSQLiteStore opens (or creates) the schema and returns a store.
func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	s := &SQLiteStore{db: db}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _wave_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL,
			body BLOB NOT NULL,
			headers TEXT NOT NULL DEFAULT '{}',
			attempts INTEGER NOT NULL DEFAULT 0,
			next_try_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			last_error TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS _wave_outbox_due
			ON _wave_outbox(next_try_at);
		CREATE TABLE IF NOT EXISTS _wave_outbox_dlq (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_id INTEGER NOT NULL,
			url TEXT NOT NULL,
			body BLOB NOT NULL,
			headers TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			dead_lettered_at INTEGER NOT NULL,
			last_error TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) DeadLetter(ctx context.Context, id int64, lastErr string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT url, body, headers, attempts, created_at
		FROM _wave_outbox WHERE id = ?`, id)
	var (
		url, hdrs string
		body      []byte
		attempts  int
		createdAt int64
	)
	if err := row.Scan(&url, &body, &hdrs, &attempts, &createdAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO _wave_outbox_dlq
		(original_id, url, body, headers, attempts, created_at, dead_lettered_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, url, body, hdrs, attempts, createdAt, time.Now().Unix(), lastErr); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM _wave_outbox WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Replay moves a single DLQ entry back into the live queue with
// attempts reset and next_try_at = now. Returns the new live id.
func (s *SQLiteStore) Replay(ctx context.Context, dlqID int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT url, body, headers FROM _wave_outbox_dlq WHERE id = ?`, dlqID)
	var (
		url, hdrs string
		body      []byte
	)
	if err := row.Scan(&url, &body, &hdrs); err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO _wave_outbox(url, body, headers, attempts, next_try_at, created_at, last_error)
		VALUES (?, ?, ?, 0, ?, ?, '')`, url, body, hdrs, now, now)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM _wave_outbox_dlq WHERE id = ?`, dlqID); err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, tx.Commit()
}

// ReplayAll drains every DLQ entry back into the live queue.
func (s *SQLiteStore) ReplayAll(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM _wave_outbox_dlq`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.Replay(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (s *SQLiteStore) DLQList(ctx context.Context, n int) ([]*Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, body, headers, attempts, created_at, last_error
		FROM _wave_outbox_dlq
		ORDER BY dead_lettered_at DESC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Delivery
	for rows.Next() {
		d := &Delivery{}
		var hdrs string
		var createdAt int64
		if err := rows.Scan(&d.ID, &d.URL, &d.Body, &hdrs, &d.Attempts, &createdAt, &d.LastError); err != nil {
			return nil, err
		}
		d.Headers = decodeHeaders(hdrs)
		d.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, d)
	}
	return out, nil
}

func (s *SQLiteStore) Enqueue(ctx context.Context, d *Delivery) error {
	hdrs := encodeHeaders(d.Headers)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO _wave_outbox(url, body, headers, attempts, next_try_at, created_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, '')`,
		d.URL, d.Body, hdrs, d.Attempts,
		d.NextTryAt.Unix(), d.CreatedAt.Unix())
	return err
}

func (s *SQLiteStore) ClaimDue(ctx context.Context, n int) ([]*Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, body, headers, attempts, next_try_at, created_at, last_error
		FROM _wave_outbox
		WHERE next_try_at <= ?
		ORDER BY next_try_at ASC
		LIMIT ?`, time.Now().Unix(), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Delivery
	for rows.Next() {
		d := &Delivery{}
		var hdrs string
		var nextTry, createdAt int64
		if err := rows.Scan(&d.ID, &d.URL, &d.Body, &hdrs, &d.Attempts, &nextTry, &createdAt, &d.LastError); err != nil {
			return nil, err
		}
		d.Headers = decodeHeaders(hdrs)
		d.NextTryAt = time.Unix(nextTry, 0)
		d.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, d)
	}
	return out, nil
}

func (s *SQLiteStore) Succeed(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM _wave_outbox WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) Fail(ctx context.Context, id int64, attempts int, nextTry time.Time, lastErr string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE _wave_outbox
		SET attempts = ?, next_try_at = ?, last_error = ?
		WHERE id = ?`, attempts, nextTry.Unix(), lastErr, id)
	return err
}

func (s *SQLiteStore) Pending(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _wave_outbox`)
	var n int
	err := row.Scan(&n)
	return n, err
}

// Headers encode/decode is a tiny `k=v\n…` line format — avoids
// pulling JSON in for what is mostly empty maps.
func encodeHeaders(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	var b bytes.Buffer
	for k, v := range m {
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	return b.String()
}

func decodeHeaders(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, line := range bytes.Split([]byte(s), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		i := bytes.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		out[string(line[:i])] = string(line[i+1:])
	}
	return out
}
