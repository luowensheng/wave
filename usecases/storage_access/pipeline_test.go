package storage_access

import (
	"encoding/json"
	"strings"
	"testing"

	"wave/io/http/contentloader"
)

func TestResolvePath(t *testing.T) {
	root := map[string]any{
		"user_id": 42,
		"user": map[string]any{
			"id":   7,
			"name": "Alice",
		},
		"orders": []any{
			map[string]any{"id": 1, "item": "Widget"},
			map[string]any{"id": 2, "item": "Gadget"},
		},
		"tags": []map[string]any{
			{"label": "vip"},
		},
	}

	cases := []struct {
		path    string
		want    any
		wantErr bool
	}{
		{"user_id", 42, false},
		{"user.id", 7, false},
		{"user.name", "Alice", false},
		{"orders.0.item", "Widget", false},
		{"orders.1.id", 2, false},
		{"tags.0.label", "vip", false},
		{"missing", nil, true},
		{"user.missing", nil, true},
		{"orders.99", nil, true},
	}

	for _, tc := range cases {
		got, err := ResolvePath(root, tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ResolvePath(%q) expected error, got %v", tc.path, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ResolvePath(%q) unexpected error: %v", tc.path, err)
			continue
		}
		// Compare using JSON encoding to handle any vs concrete types.
		gotJ, _ := json.Marshal(got)
		wantJ, _ := json.Marshal(tc.want)
		if string(gotJ) != string(wantJ) {
			t.Errorf("ResolvePath(%q) = %s, want %s", tc.path, gotJ, wantJ)
		}
	}
}

func TestToSQLParam(t *testing.T) {
	// Scalars pass through unchanged.
	cases := []struct {
		in  any
		out any
	}{
		{nil, nil},
		{"hello", "hello"},
		{42, 42},
		{int64(99), int64(99)},
		{3.14, 3.14},
		{true, true},
	}
	for _, tc := range cases {
		got := ToSQLParam(tc.in)
		gotJ, _ := json.Marshal(got)
		wantJ, _ := json.Marshal(tc.out)
		if string(gotJ) != string(wantJ) {
			t.Errorf("ToSQLParam(%v) = %v, want %v", tc.in, got, tc.out)
		}
	}

	// Non-scalars are JSON-encoded.
	m := map[string]any{"a": 1}
	got := ToSQLParam(m)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("ToSQLParam(map) should return string, got %T", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(s), &decoded); err != nil {
		t.Errorf("ToSQLParam(map) returned invalid JSON: %v", err)
	}

	sl := []any{1, 2, 3}
	got = ToSQLParam(sl)
	s, ok = got.(string)
	if !ok {
		t.Fatalf("ToSQLParam(slice) should return string, got %T", got)
	}
	if s != "[1,2,3]" {
		t.Errorf("ToSQLParam(slice) = %q, want %q", s, "[1,2,3]")
	}
}

// stubSt is a minimal StorageRef for pipeline boot-validation tests.
// It records Execute calls but returns nil without touching a real DB.
type stubSt struct {
	executed []string
}

func (s *stubSt) Execute(cmd string, dl *contentloader.DataLoader) (any, error) {
	s.executed = append(s.executed, cmd)
	return nil, nil
}

// TestResolvePath_InputsNamespace verifies that ResolvePath correctly navigates
// accum["inputs"] (the namespace seeded from declared route inputs in pipeline mode)
// and other step result namespaces.
func TestResolvePath_InputsNamespace(t *testing.T) {
	root := map[string]any{
		"inputs": map[string]any{
			"user_id": 42,
			"city":    "London",
		},
		"weather": map[string]any{
			"temp": 18.5,
			"body": `{"temp":18.5}`,
		},
	}

	cases := []struct {
		path string
		want any
	}{
		{"inputs.user_id", 42},
		{"inputs.city", "London"},
		{"weather.temp", 18.5},
		{"weather.body", `{"temp":18.5}`},
	}

	for _, tc := range cases {
		got, err := ResolvePath(root, tc.path)
		if err != nil {
			t.Errorf("ResolvePath(%q) unexpected error: %v", tc.path, err)
			continue
		}
		gotJ, _ := json.Marshal(got)
		wantJ, _ := json.Marshal(tc.want)
		if string(gotJ) != string(wantJ) {
			t.Errorf("ResolvePath(%q) = %s, want %s", tc.path, gotJ, wantJ)
		}
	}
}

// TestPipelineBootValidation_EmptyFromPath verifies that Config.CreateRoute (pipeline
// mode) returns a boot-time error when a step declares an input with an empty from-path.
func TestPipelineBootValidation_EmptyFromPath(t *testing.T) {
	stub := &stubSt{}
	oldGetStorageFn := GetStorageFn
	GetStorageFn = func(name string) (StorageRef, bool) {
		if name == "db" {
			return stub, true
		}
		return nil, false
	}
	defer func() { GetStorageFn = oldGetStorageFn }()

	cfg := &Config{
		Steps: []PipelineStep{
			{
				Source:  "db",
				Execute: "SELECT 1",
				As:      "result",
				Inputs:  map[string]string{"user_id": ""},
			},
		},
		OutputTemplate:      `{{toJSON .result}}`,
		ResponseContentType: "application/json",
	}

	_, err := cfg.CreateRoute("GET", "/test", nil)
	if err == nil {
		t.Fatal("expected boot error for empty from-path, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected 'from-path is empty' in error message, got: %v", err)
	}
}
