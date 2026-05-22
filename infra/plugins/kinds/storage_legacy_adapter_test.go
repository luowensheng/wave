package kinds

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/luowensheng/wave/io/http/contentloader"
)

// fakeStorage is a programmable StoragePlugin for adapter tests.
type fakeStorage struct {
	gotSQL string
	res    *QueryResult
	err    error
}

func (f *fakeStorage) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (f *fakeStorage) Set(context.Context, string, []byte) error         { return nil }
func (f *fakeStorage) Delete(context.Context, string) error              { return nil }
func (f *fakeStorage) Migrate(context.Context, *MigrationPlan) error     { return nil }
func (f *fakeStorage) Close() error                                      { return nil }

func (f *fakeStorage) Query(_ context.Context, q *Query) (*QueryResult, error) {
	f.gotSQL = q.SQL
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func newDataLoader(values map[string]any) *contentloader.DataLoader {
	r := httptest.NewRequest("GET", "/", nil)
	return contentloader.NewDataLoaderFromContentLoader(r, contentloader.NewInputsLoader(values))
}

func TestStorageRefAdapter_Execute(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		values   map[string]any
		fake     *fakeStorage
		want     any
		wantSQL  string
		wantErr  bool
	}{
		{
			name:   "select single row returns map",
			sql:    `SELECT * FROM users WHERE id = {{.id}}`,
			values: map[string]any{"id": 7},
			fake: &fakeStorage{res: &QueryResult{
				Columns: []string{"id", "name"},
				Rows:    []map[string]any{{"id": int64(7), "name": "alice"}},
			}},
			want:    map[string]any{"id": int64(7), "name": "alice"},
			wantSQL: "SELECT * FROM users WHERE id = 7",
		},
		{
			name: "select multi row returns slice",
			sql:  `SELECT * FROM users`,
			fake: &fakeStorage{res: &QueryResult{
				Columns: []string{"id"},
				Rows: []map[string]any{
					{"id": int64(1)},
					{"id": int64(2)},
				},
			}},
			want: []map[string]any{{"id": int64(1)}, {"id": int64(2)}},
		},
		{
			name: "select scalar single col single row",
			sql:  `SELECT count(*) FROM users`,
			fake: &fakeStorage{res: &QueryResult{
				Columns: []string{"count"},
				Rows:    []map[string]any{{"count": int64(42)}},
			}},
			want: int64(42),
		},
		{
			name: "select empty returns empty slice",
			sql:  `SELECT * FROM users`,
			fake: &fakeStorage{res: &QueryResult{Columns: []string{"id"}, Rows: nil}},
			want: []map[string]any{},
		},
		{
			name:   "insert returns id and rows_affected",
			sql:    `INSERT INTO users(name) VALUES('{{.name}}')`,
			values: map[string]any{"name": "bob"},
			fake: &fakeStorage{res: &QueryResult{
				LastInsertID: 99,
				RowsAffected: 1,
			}},
			want:    map[string]any{"id": int64(99), "rows_affected": int64(1)},
			wantSQL: "INSERT INTO users(name) VALUES('bob')",
		},
		{
			name: "update returns rows_affected only",
			sql:  `UPDATE users SET name = 'x'`,
			fake: &fakeStorage{res: &QueryResult{RowsAffected: 3}},
			want: map[string]any{"rows_affected": int64(3)},
		},
		{
			name: "delete returns rows_affected only",
			sql:  `DELETE FROM users`,
			fake: &fakeStorage{res: &QueryResult{RowsAffected: 5}},
			want: map[string]any{"rows_affected": int64(5)},
		},
		{
			name: "exec / DDL returns nil",
			sql:  `CREATE TABLE x (id INT)`,
			fake: &fakeStorage{res: &QueryResult{}},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &StorageRefAdapter{Plugin: tc.fake}
			got, err := a.Execute(tc.sql, newDataLoader(tc.values))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("result mismatch: got %#v want %#v", got, tc.want)
			}
			if tc.wantSQL != "" && tc.fake.gotSQL != tc.wantSQL {
				t.Errorf("rendered SQL mismatch: got %q want %q", tc.fake.gotSQL, tc.wantSQL)
			}
		})
	}
}

func TestStorageRefAdapter_NilPlugin(t *testing.T) {
	a := &StorageRefAdapter{}
	if _, err := a.Execute("SELECT 1", newDataLoader(nil)); err == nil {
		t.Fatal("expected error for nil plugin")
	}
}

func TestDetermineQueryType(t *testing.T) {
	cases := map[string]SQLQueryType{
		"":                                queryTypeExec,
		"  select * from t":                queryTypeSelect,
		"INSERT INTO t VALUES (1)":         queryTypeInsert,
		"\nupdate t set x=1":               queryTypeUpdate,
		"DELETE FROM t":                    queryTypeDelete,
		"CREATE TABLE t (id INT)":          queryTypeExec,
	}
	for sql, want := range cases {
		if got := determineQueryType(sql); got != want {
			t.Errorf("%q: got %d want %d", sql, got, want)
		}
	}
}
