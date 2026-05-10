package servers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"wave/infra/plugins/kinds"
	storageaccess "wave/usecases/storage_access"
)

// fakeStorage is an in-memory StoragePlugin used by the wiring test. It
// supports the SELECT/INSERT/DELETE shapes the route exercises and
// records every SQL statement it received.
type fakeStorage struct {
	mu    sync.Mutex
	store map[string]string // key=name → value=email, just to have something to query
	saw   []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{store: map[string]string{}}
}

func (f *fakeStorage) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (f *fakeStorage) Set(context.Context, string, []byte) error         { return nil }
func (f *fakeStorage) Delete(context.Context, string) error              { return nil }
func (f *fakeStorage) Migrate(context.Context, *kinds.MigrationPlan) error {
	return nil
}
func (f *fakeStorage) Close() error { return nil }

func (f *fakeStorage) Query(_ context.Context, q *kinds.Query) (*kinds.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saw = append(f.saw, q.SQL)
	upper := strings.ToUpper(strings.TrimSpace(q.SQL))
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		rows := []map[string]any{}
		for k, v := range f.store {
			rows = append(rows, map[string]any{"name": k, "email": v})
		}
		return &kinds.QueryResult{Columns: []string{"name", "email"}, Rows: rows}, nil
	case strings.HasPrefix(upper, "INSERT"):
		// Pull the VALUES(...) tuple out of the rendered SQL.
		v := strings.Index(upper, "VALUES")
		if v == -1 {
			return &kinds.QueryResult{RowsAffected: 0}, nil
		}
		tail := q.SQL[v+len("VALUES"):]
		left := strings.Index(tail, "(")
		right := strings.LastIndex(tail, ")")
		if left == -1 || right == -1 || right <= left {
			return &kinds.QueryResult{RowsAffected: 0}, nil
		}
		parts := strings.Split(tail[left+1:right], ",")
		if len(parts) != 2 {
			return &kinds.QueryResult{RowsAffected: 0}, nil
		}
		name := strings.Trim(strings.TrimSpace(parts[0]), "'\"")
		email := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		f.store[name] = email
		return &kinds.QueryResult{LastInsertID: int64(len(f.store)), RowsAffected: 1}, nil
	}
	return &kinds.QueryResult{}, nil
}

// installFakeStorage wires a single fake plugin into the storage_access
// lookup. Restores the previous fn on cleanup so tests don't leak into
// each other.
func installFakeStorage(t *testing.T, name string, fake kinds.StoragePlugin) {
	t.Helper()
	prev := storageaccess.GetStorageFn
	storageaccess.GetStorageFn = func(n string) (storageaccess.StorageRef, bool) {
		if n == name {
			return &kinds.StorageRefAdapter{Plugin: fake}, true
		}
		return nil, false
	}
	t.Cleanup(func() { storageaccess.GetStorageFn = prev })
}

func TestStoragePluginRoute_SelectAndInsert(t *testing.T) {
	fake := newFakeStorage()
	fake.store["alice"] = "alice@example.com"
	installFakeStorage(t, "pg_main", fake)

	// GET (SELECT) ----------------------------------------------------
	getCfg := &storageaccess.Config{
		Source:              "pg_main",
		Execute:             "SELECT name, email FROM users",
		OutputTemplate:      "{{toJSON .}}",
		ResponseContentType: "application/json",
	}
	getHandler, err := getCfg.CreateRoute("GET", "/users", nil)
	if err != nil {
		t.Fatalf("CreateRoute(GET): %v", err)
	}

	rr := httptest.NewRecorder()
	getHandler(rr, httptest.NewRequest("GET", "/users", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rr.Code, rr.Body.String())
	}
	body, _ := io.ReadAll(rr.Body)
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		// Adapter returns a single map for the 1-row case
		var single map[string]any
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			t.Fatalf("could not decode body %q: list err=%v map err=%v", body, err, err2)
		}
		if single["name"] != "alice" {
			t.Fatalf("unexpected row: %+v", single)
		}
	} else if len(rows) > 0 && rows[0]["name"] != "alice" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	// POST (INSERT) ---------------------------------------------------
	postCfg := &storageaccess.Config{
		Source:              "pg_main",
		Execute:             "INSERT INTO users(name, email) VALUES('{{.name}}', '{{.email}}')",
		OutputTemplate:      "{{toJSON .}}",
		ResponseContentType: "application/json",
		ExpectedContentType: "application/x-www-form-urlencoded",
	}
	postHandler, err := postCfg.CreateRoute("POST", "/users", nil)
	if err != nil {
		t.Fatalf("CreateRoute(POST): %v", err)
	}
	form := strings.NewReader("name=bob&email=bob@example.com")
	req := httptest.NewRequest("POST", "/users", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	postHandler(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rr2.Code, rr2.Body.String())
	}

	if got, ok := fake.store["bob"]; !ok || got != "bob@example.com" {
		t.Fatalf("INSERT did not land in fake store: %+v", fake.store)
	}

	// Confirm the SELECT and the INSERT were both seen by the plugin.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.saw) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(fake.saw), fake.saw)
	}
}

func TestStoragePluginRoute_UnknownSourceFailsCreateRoute(t *testing.T) {
	installFakeStorage(t, "pg_main", newFakeStorage())
	cfg := &storageaccess.Config{
		Source:              "missing",
		Execute:             "SELECT 1",
		OutputTemplate:      "x",
		ResponseContentType: "text/plain",
	}
	if _, err := cfg.CreateRoute("GET", "/x", nil); err == nil {
		t.Fatal("expected CreateRoute to fail for unknown source")
	}
}
