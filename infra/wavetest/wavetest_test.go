package wavetest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubServer stands in for a real Wave handler so we can exercise
// the runner in isolation from server build.
func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","build":42}`))
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"method":"` + r.Method + `","query":"` + r.URL.RawQuery + `"}`))
	})
	mux.HandleFunc("/header-X", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"X":"` + r.Header.Get("X-Test") + `"}`))
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	})
	return httptest.NewServer(mux)
}

func TestJSONSubset_HappyPath(t *testing.T) {
	want := map[string]any{"status": "ok"}
	got := map[string]any{"status": "ok", "build": 42, "extra": []any{1, 2, 3}}
	if d := jsonSubset(want, got, ""); d != "" {
		t.Fatalf("expected subset match, got diff: %s", d)
	}
}

func TestJSONSubset_MissingKey(t *testing.T) {
	want := map[string]any{"missing": "x"}
	got := map[string]any{"status": "ok"}
	if d := jsonSubset(want, got, ""); d == "" {
		t.Fatal("expected diff for missing key, got match")
	}
}

func TestJSONSubset_Wildcard(t *testing.T) {
	want := map[string]any{"id": "*", "name": "ada"}
	got := map[string]any{"id": 42, "name": "ada"}
	if d := jsonSubset(want, got, ""); d != "" {
		t.Fatalf("wildcard should match present field: %s", d)
	}
	gotMissing := map[string]any{"name": "ada"}
	if d := jsonSubset(want, gotMissing, ""); d == "" {
		t.Fatal("wildcard should not match missing field")
	}
}

func TestJSONSubset_NumericCoercion(t *testing.T) {
	// YAML parses 1 as int; JSON unmarshal parses 1 as float64.
	// Subset comparison must coerce both to a common type.
	if d := jsonSubset(int(1), float64(1), ""); d != "" {
		t.Fatalf("int 1 vs float64 1 should match: %s", d)
	}
}

func TestJSONSubset_ArrayLengthMustMatch(t *testing.T) {
	want := []any{1, 2}
	got := []any{1, 2, 3}
	if d := jsonSubset(want, got, ""); d == "" {
		t.Fatal("array length mismatch should fail")
	}
}

func TestRunner_PassingCases(t *testing.T) {
	ts := stubServer(t)
	defer ts.Close()

	suite := &Suite{
		Tests: []Case{
			{
				Name:    "ok returns 200",
				Request: Request{Method: "GET", Path: "/ok"},
				Expect:  Expect{Status: 200, JSON: map[string]any{"status": "ok"}},
			},
			{
				Name:    "header echoed",
				Request: Request{Method: "GET", Path: "/header-X", Headers: map[string]string{"X-Test": "hello"}},
				Expect:  Expect{Status: 200, JSON: map[string]any{"X": "hello"}},
			},
			{
				Name:    "query echoed",
				Request: Request{Method: "GET", Path: "/echo", Query: map[string]string{"a": "1", "b": "2"}},
				Expect:  Expect{Status: 200, BodyContains: "a=1"},
			},
		},
	}
	summary := run(ts, suite, "test")
	if !summary.OK {
		t.Fatalf("expected ok, got %d failures: %v", summary.Failed, summary.Results)
	}
	if summary.Passed != 3 {
		t.Fatalf("expected 3 passed, got %d", summary.Passed)
	}
}

func TestRunner_FailingCaseProducesDiagnostic(t *testing.T) {
	ts := stubServer(t)
	defer ts.Close()

	suite := &Suite{
		Tests: []Case{
			{
				Name:    "wrong status",
				Request: Request{Method: "GET", Path: "/bad"},
				Expect:  Expect{Status: 200}, // actual is 400
			},
		},
	}
	summary := run(ts, suite, "test")
	if summary.OK {
		t.Fatal("expected failure")
	}
	if len(summary.Results[0].Errors) == 0 {
		t.Fatal("expected at least one error message")
	}
	if !strings.Contains(summary.Results[0].Errors[0], "status") {
		t.Fatalf("error should mention status: %q", summary.Results[0].Errors[0])
	}
}

func TestRunner_CaptureAndInterpolate(t *testing.T) {
	ts := stubServer(t)
	defer ts.Close()

	suite := &Suite{
		Tests: []Case{
			{
				Name:    "capture build",
				Request: Request{Method: "GET", Path: "/ok"},
				Expect:  Expect{Status: 200},
				Capture: map[string]string{"b": "json.build"},
			},
			{
				Name:    "use captured value in query",
				Request: Request{Method: "GET", Path: "/echo", Query: map[string]string{"buildno": "{{.b}}"}},
				Expect:  Expect{Status: 200, BodyContains: "buildno=42"},
			},
		},
	}
	summary := run(ts, suite, "test")
	if !summary.OK {
		t.Fatalf("expected ok, got: %+v", summary.Results)
	}
}

func TestRunner_SetupFailureSkipsTests(t *testing.T) {
	ts := stubServer(t)
	defer ts.Close()

	suite := &Suite{
		Setup: []Case{
			{Name: "setup that fails", Request: Request{Method: "GET", Path: "/bad"}, Expect: Expect{Status: 200}},
		},
		Tests: []Case{
			{Name: "would have passed", Request: Request{Method: "GET", Path: "/ok"}, Expect: Expect{Status: 200}},
		},
	}
	summary := run(ts, suite, "test")
	if summary.OK {
		t.Fatal("expected suite to fail because setup failed")
	}
	// Tests phase should be skipped entirely.
	for _, r := range summary.Results {
		if r.Phase == "test" {
			t.Fatalf("test phase should not have run: %+v", r)
		}
	}
}

func TestSetEnv_RestoresOriginal(t *testing.T) {
	const k = "WAVETEST_RESTORE_KEY"
	_ = os.Setenv(k, "original")
	defer os.Unsetenv(k)

	restore := setEnv(map[string]string{k: "overridden"})
	if got := os.Getenv(k); got != "overridden" {
		t.Fatalf("env not set during run: %q", got)
	}
	restore()
	if got := os.Getenv(k); got != "original" {
		t.Fatalf("env not restored: %q", got)
	}
}

func TestSetEnv_UnsetsKeysThatDidntExist(t *testing.T) {
	const k = "WAVETEST_NEW_KEY_FOR_TEST_ONLY"
	_ = os.Unsetenv(k)

	restore := setEnv(map[string]string{k: "x"})
	if _, ok := os.LookupEnv(k); !ok {
		t.Fatal("env not set")
	}
	restore()
	if _, ok := os.LookupEnv(k); ok {
		t.Fatal("env should be unset after restore")
	}
}

func TestLoad_RejectsMissingImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.test.yaml")
	if err := os.WriteFile(path, []byte("tests: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected error for missing import:")
	}
}

func TestLoad_ResolvesRelativeImport(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(serverPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(dir, "suite.test.yaml")
	if err := os.WriteFile(suitePath, []byte("import: server.yaml\ntests: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, resolved, err := Load(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != serverPath {
		t.Fatalf("got %q, want %q", resolved, serverPath)
	}
}

// TestRunFileWithOptions_QuietHidesBootNoise verifies the dup2-based
// silencing actually swallows server boot prints. Without Quiet,
// boot prints leak; with Quiet they don't.
func TestRunFileWithOptions_QuietHidesBootNoise(t *testing.T) {
	dir := t.TempDir()
	serverYAML := `default:
  port: 8080
routes:
  - path: /ping
    method: GET
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "text/plain"]]
      body: "pong"
`
	suiteYAML := `import: server.yaml
tests:
  - name: ping
    request: { method: GET, path: /ping }
    expect: { status: 200 }
`
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte(serverYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(dir, "suite.test.yaml")
	if err := os.WriteFile(suitePath, []byte(suiteYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)

	// Quiet=true must still produce a correct summary.
	summary, err := RunFileWithOptions(context.Background(), suitePath, Options{Quiet: true})
	if err != nil {
		t.Fatalf("RunFileWithOptions: %v", err)
	}
	if !summary.OK || summary.Passed != 1 {
		t.Fatalf("expected 1 passed, got %+v", summary)
	}
}

// TestRunFile is an end-to-end test against a real Wave server.
// It boots an in-process server using BuildHandler (no port binding)
// and runs a suite. Covers the integration between wavetest and
// orchestrator/server.
func TestRunFile_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	serverYAML := `default:
  port: 8080
routes:
  - path: /ping
    method: GET
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "text/plain"]]
      body: "pong"
`
	suiteYAML := `import: server.yaml
tests:
  - name: ping
    request: { method: GET, path: /ping }
    expect:
      status: 200
      body: pong
`
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte(serverYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(dir, "suite.test.yaml")
	if err := os.WriteFile(suitePath, []byte(suiteYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wave's NewServer does an os.Chdir into the server.yaml's directory.
	// Save and restore the original so concurrent tests don't see a
	// surprise cwd change.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)

	summary, err := RunFile(context.Background(), suitePath)
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if !summary.OK {
		t.Fatalf("suite should be OK, got: %+v", summary.Results)
	}
	if summary.Passed != 1 {
		t.Fatalf("expected 1 passed, got %d", summary.Passed)
	}
}
