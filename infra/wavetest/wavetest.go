// Package wavetest is the in-process functional-test runner for
// Wave servers. It loads a `.test.yaml` (which imports a
// `server.yaml`), boots that server **without binding a port**
// using Server.BuildHandler, fronts the handler with
// httptest.NewServer, and walks a sequence of `setup → tests →
// teardown` cases, asserting status + body + JSON subset shape.
//
// The runner is invoked by:
//
//   - `wave test <suite.yaml>` (orchestrator/main.go)
//   - Go tests via wavetest.Run(t, "path/to/suite.yaml")
//
// Design goals:
//
//   - YAML-driven. Wave's audience is YAML-fluent; a third language
//     for tests would be friction.
//   - Strict-subset JSON matching. `expect.json` lists fields you
//     care about; extra keys in the response are fine.
//   - Variable capture between cases. `capture: { id: json.id }`
//     stores the value at `json.id`; later cases template against
//     `{{.id}}` in path / body / headers / query.
//   - No port binding. Every suite is hermetic; runs in parallel
//     in CI without port collisions.
package wavetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"time"

	servers "github.com/luowensheng/wave/orchestrator/server"
	"gopkg.in/yaml.v3"
)

// Suite is the top-level shape of a `.test.yaml`.
type Suite struct {
	// Import is the relative path to the server.yaml under test.
	// Resolved relative to the suite file's directory.
	Import string `yaml:"import"`

	// Env values are exported into the process for the duration
	// of the run, so server.yaml `${env:NAME}` interpolation
	// resolves them. Restored on completion.
	Env map[string]string `yaml:"env,omitempty"`

	// Setup cases run before Tests; if any fails, Tests are skipped.
	Setup []Case `yaml:"setup,omitempty"`

	// Tests are the assertions. A failure in one doesn't stop
	// subsequent cases unless they reference captured vars that
	// weren't set.
	Tests []Case `yaml:"tests,omitempty"`

	// Teardown cases run after Tests regardless of outcome. Failures
	// here are reported but don't change the overall pass/fail.
	Teardown []Case `yaml:"teardown,omitempty"`
}

// Case is one request/assert pair.
type Case struct {
	Name    string            `yaml:"name"`
	Request Request           `yaml:"request"`
	Expect  Expect            `yaml:"expect,omitempty"`
	Capture map[string]string `yaml:"capture,omitempty"` // var → dot-path into response
}

// Request describes the outbound HTTP request.
//
// Path / Body / Headers / Query / Form values pass through
// text/template with the captured-vars map as data, so earlier
// captures can be referenced as `{{.var_name}}`.
type Request struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Query   map[string]string `yaml:"query,omitempty"`
	Body    string            `yaml:"body,omitempty"` // raw string body
	JSON    any               `yaml:"json,omitempty"` // marshaled with Content-Type: application/json
	Form    map[string]string `yaml:"form,omitempty"` // x-www-form-urlencoded
}

// Expect lists the assertions to run against the response.
// Each field is optional; an empty Expect asserts only that the
// request didn't error.
type Expect struct {
	// Status — exact HTTP status code (0 = don't check)
	Status int `yaml:"status,omitempty"`

	// Body — exact match (after TrimSpace). Use BodyContains for substring.
	Body string `yaml:"body,omitempty"`

	// BodyContains — substring match. Cheaper than crafting a regex.
	BodyContains string `yaml:"body_contains,omitempty"`

	// Headers — case-insensitive name → exact value.
	Headers map[string]string `yaml:"headers,omitempty"`

	// JSON — strict subset match against the response body parsed
	// as JSON. Maps are matched recursively (extra keys OK); slices
	// must have the same length and each element matches by index.
	// Use the literal string "*" as a value to assert "field exists,
	// any value".
	JSON any `yaml:"json,omitempty"`
}

// Result is what each case produces. Field tags are designed for
// machine consumption (`wave test --json`).
type Result struct {
	Name     string        `json:"name"`
	Phase    string        `json:"phase"` // "setup" | "test" | "teardown"
	Passed   bool          `json:"passed"`
	Status   int           `json:"status"`
	Errors   []string      `json:"errors,omitempty"`
	Duration time.Duration `json:"duration_ns"`
}

// Summary is the overall outcome for a suite.
type Summary struct {
	Suite    string   `json:"suite"`
	Results  []Result `json:"results"`
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Duration float64  `json:"duration_seconds"`
	OK       bool     `json:"ok"`
}

// Load reads a suite YAML from disk. The Import path inside the
// suite is resolved relative to the suite file's directory.
func Load(suitePath string) (*Suite, string, error) {
	raw, err := os.ReadFile(suitePath)
	if err != nil {
		return nil, "", err
	}
	var s Suite
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, "", fmt.Errorf("suite: yaml parse: %w", err)
	}
	if s.Import == "" {
		return nil, "", fmt.Errorf("suite: missing required `import:` (path to server.yaml)")
	}
	resolved := s.Import
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(suitePath), resolved)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return nil, "", err
	}
	return &s, abs, nil
}

// Options controls runner behavior.
type Options struct {
	// Quiet silences server boot prints + per-request access logs
	// + stdlib log output for the duration of the run, by
	// redirecting fd 1 and fd 2 to /dev/null via syscall.Dup2.
	// Restored before RunFileWithOptions returns. Unix-only —
	// no-op on Windows (use --verbose there to see all output).
	Quiet bool
}

// RunFile loads a suite, builds the imported Wave server, and runs
// every case with default options. Returns the Summary regardless
// of pass/fail; check .OK or .Failed to decide exit code.
func RunFile(ctx context.Context, suitePath string) (*Summary, error) {
	return RunFileWithOptions(ctx, suitePath, Options{})
}

// RunFileWithOptions is RunFile with explicit options. The CLI
// `wave test` calls this with Quiet=true unless --verbose is set,
// so its pass/fail summary isn't drowned by the server's boot
// prints + access logs. Go tests typically pass Options{} so they
// can see the logs in -v output.
func RunFileWithOptions(ctx context.Context, suitePath string, opts Options) (*Summary, error) {
	suite, serverPath, err := Load(suitePath)
	if err != nil {
		return nil, err
	}

	// Export suite-declared env vars for the lifetime of the run so
	// server.yaml ${env:NAME} interpolation finds them. Restore on
	// exit.
	restoreEnv := setEnv(suite.Env)
	defer restoreEnv()

	if opts.Quiet {
		restoreOut := silenceOutput()
		defer restoreOut()
	}

	// Boot the server WITHOUT binding a port.
	srv, err := servers.NewServer(serverPath)
	if err != nil {
		return nil, fmt.Errorf("server build: %w", err)
	}
	handler, err := srv.BuildHandler(ctx)
	if err != nil {
		return nil, fmt.Errorf("server handler: %w", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	return run(ts, suite, suitePath), nil
}

func run(ts *httptest.Server, suite *Suite, suitePath string) *Summary {
	start := time.Now()
	vars := map[string]any{}
	var results []Result

	runPhase := func(cases []Case, phase string, stopOnFail bool) bool {
		ok := true
		for _, c := range cases {
			r := runCase(ts, c, phase, vars)
			results = append(results, r)
			if !r.Passed {
				ok = false
				if stopOnFail {
					return false
				}
			}
		}
		return ok
	}

	setupOK := runPhase(suite.Setup, "setup", true)
	if setupOK {
		runPhase(suite.Tests, "test", false)
	}
	runPhase(suite.Teardown, "teardown", false)

	passed, failed := 0, 0
	for _, r := range results {
		// Teardown failures are reported but don't fail the suite.
		if r.Phase == "teardown" {
			if r.Passed {
				passed++
			}
			continue
		}
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}

	return &Summary{
		Suite:    suitePath,
		Results:  results,
		Passed:   passed,
		Failed:   failed,
		Duration: time.Since(start).Seconds(),
		OK:       failed == 0 && setupOK,
	}
}

func runCase(ts *httptest.Server, c Case, phase string, vars map[string]any) Result {
	start := time.Now()
	res := Result{Name: c.Name, Phase: phase, Passed: true}

	httpReq, err := buildRequest(ts.URL, c.Request, vars)
	if err != nil {
		res.Passed = false
		res.Errors = append(res.Errors, fmt.Sprintf("build request: %v", err))
		res.Duration = time.Since(start)
		return res
	}

	httpResp, err := ts.Client().Do(httpReq)
	if err != nil {
		res.Passed = false
		res.Errors = append(res.Errors, fmt.Sprintf("http: %v", err))
		res.Duration = time.Since(start)
		return res
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	res.Status = httpResp.StatusCode

	// Apply assertions.
	if errs := assertExpect(c.Expect, httpResp, body); len(errs) > 0 {
		res.Passed = false
		res.Errors = append(res.Errors, errs...)
	}

	// Apply captures (even when assertions failed — sometimes the
	// next case wants to inspect the failure state).
	if len(c.Capture) > 0 {
		var jsonBody any
		_ = json.Unmarshal(body, &jsonBody)
		for varName, path := range c.Capture {
			val, ok := navigatePath(jsonBody, path, httpResp)
			if !ok {
				res.Errors = append(res.Errors,
					fmt.Sprintf("capture %q: path %q not found in response", varName, path))
				res.Passed = false
				continue
			}
			vars[varName] = val
		}
	}

	res.Duration = time.Since(start)
	return res
}

// buildRequest templates the request fields with vars and constructs
// the *http.Request pointed at the httptest server's base URL.
func buildRequest(base string, r Request, vars map[string]any) (*http.Request, error) {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}

	path, err := tmpl(r.Path, vars)
	if err != nil {
		return nil, fmt.Errorf("path template: %w", err)
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	u, err := url.Parse(base + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if len(r.Query) > 0 {
		q := u.Query()
		for k, v := range r.Query {
			rendered, err := tmpl(v, vars)
			if err != nil {
				return nil, fmt.Errorf("query[%q]: %w", k, err)
			}
			q.Set(k, rendered)
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	contentType := ""
	switch {
	case r.JSON != nil:
		// Template-walk the JSON tree so {{.var}} works inside any string field.
		walked, err := walkAndTemplate(r.JSON, vars)
		if err != nil {
			return nil, fmt.Errorf("json body template: %w", err)
		}
		buf, err := json.Marshal(walked)
		if err != nil {
			return nil, fmt.Errorf("marshal json body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
		contentType = "application/json"
	case len(r.Form) > 0:
		form := url.Values{}
		for k, v := range r.Form {
			rendered, err := tmpl(v, vars)
			if err != nil {
				return nil, fmt.Errorf("form[%q]: %w", k, err)
			}
			form.Set(k, rendered)
		}
		bodyReader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	case r.Body != "":
		rendered, err := tmpl(r.Body, vars)
		if err != nil {
			return nil, fmt.Errorf("body template: %w", err)
		}
		bodyReader = strings.NewReader(rendered)
	}

	req, err := http.NewRequest(method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range r.Headers {
		rendered, err := tmpl(v, vars)
		if err != nil {
			return nil, fmt.Errorf("header[%q]: %w", k, err)
		}
		req.Header.Set(k, rendered)
	}
	return req, nil
}

// assertExpect runs every assertion in Expect against the response.
// Returns a list of human-readable error messages; empty list = pass.
func assertExpect(e Expect, resp *http.Response, body []byte) []string {
	var errs []string

	if e.Status != 0 && resp.StatusCode != e.Status {
		errs = append(errs,
			fmt.Sprintf("status: got %d, want %d (body=%s)",
				resp.StatusCode, e.Status, snippet(body)))
	}

	if e.Body != "" {
		got := strings.TrimSpace(string(body))
		want := strings.TrimSpace(e.Body)
		if got != want {
			errs = append(errs, fmt.Sprintf("body: got %q, want %q", got, want))
		}
	}

	if e.BodyContains != "" && !strings.Contains(string(body), e.BodyContains) {
		errs = append(errs,
			fmt.Sprintf("body_contains: %q not found in %s",
				e.BodyContains, snippet(body)))
	}

	for k, v := range e.Headers {
		got := resp.Header.Get(k)
		if got != v {
			errs = append(errs,
				fmt.Sprintf("header[%q]: got %q, want %q", k, got, v))
		}
	}

	if e.JSON != nil {
		var got any
		if err := json.Unmarshal(body, &got); err != nil {
			errs = append(errs, fmt.Sprintf("json: response is not JSON: %v (body=%s)", err, snippet(body)))
		} else if diff := jsonSubset(e.JSON, got, ""); diff != "" {
			errs = append(errs, "json: "+diff)
		}
	}

	return errs
}

// jsonSubset asserts that `want` is a strict subset of `got`. Maps:
// every key in want must exist in got with matching value (recursive);
// extra keys in got are fine. Slices: same length, element-wise subset
// by index. Scalars: equal (with int/float/string flexibility).
// The literal want value "*" matches any present field — for "this
// field exists, I don't care about the value" assertions.
func jsonSubset(want, got any, path string) string {
	if want == nil {
		return ""
	}
	// "*" wildcard — any value passes.
	if s, ok := want.(string); ok && s == "*" {
		if got == nil {
			return fmt.Sprintf("at %s: expected any value, got null", pathOrRoot(path))
		}
		return ""
	}

	switch w := want.(type) {
	case map[string]any:
		gm, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("at %s: expected object, got %T", pathOrRoot(path), got)
		}
		// Sort keys for deterministic error messages.
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if d := jsonSubset(w[k], gm[k], path+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		ga, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("at %s: expected array, got %T", pathOrRoot(path), got)
		}
		if len(w) != len(ga) {
			return fmt.Sprintf("at %s: array length %d, want %d",
				pathOrRoot(path), len(ga), len(w))
		}
		for i := range w {
			if d := jsonSubset(w[i], ga[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	default:
		if !scalarEqual(w, got) {
			return fmt.Sprintf("at %s: got %v (%T), want %v (%T)",
				pathOrRoot(path), got, got, w, w)
		}
		return ""
	}
}

// scalarEqual handles int↔float quirks (YAML parses 1 as int, JSON
// numbers parse as float64).
func scalarEqual(want, got any) bool {
	if reflect.DeepEqual(want, got) {
		return true
	}
	// numeric coercion
	wf, wok := toFloat64(want)
	gf, gok := toFloat64(got)
	if wok && gok {
		return wf == gf
	}
	// string coercion (e.g. YAML may parse "true" as bool)
	return fmt.Sprintf("%v", want) == fmt.Sprintf("%v", got)
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}

// navigatePath drills into a parsed-JSON tree by dot-path. Supports
// numeric indices for slices ("items.0.name") and string keys for
// maps. Also supports the special prefix "header:NAME" to read a
// response header instead.
func navigatePath(jsonBody any, path string, resp *http.Response) (any, bool) {
	if strings.HasPrefix(path, "header:") {
		v := resp.Header.Get(strings.TrimPrefix(path, "header:"))
		return v, v != ""
	}
	if strings.HasPrefix(path, "status") {
		return resp.StatusCode, true
	}
	// "json.field.0.x" — strip the leading "json." if present
	path = strings.TrimPrefix(path, "json.")
	if path == "" || path == "json" {
		return jsonBody, jsonBody != nil
	}

	var cur any = jsonBody
	for _, seg := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			var idx int
			if _, err := fmt.Sscanf(seg, "%d", &idx); err != nil {
				return nil, false
			}
			if idx < 0 || idx >= len(c) {
				return nil, false
			}
			cur = c[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// tmpl renders a string through text/template with vars as data.
// Empty string passes through unchanged.
func tmpl(s string, vars map[string]any) (string, error) {
	if s == "" || !strings.Contains(s, "{{") {
		return s, nil
	}
	t, err := template.New("").Parse(s)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// walkAndTemplate walks an arbitrary tree (the kind yaml.Unmarshal
// produces for the JSON body field) and templates every string leaf.
func walkAndTemplate(v any, vars map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		return tmpl(x, vars)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			w, err := walkAndTemplate(val, vars)
			if err != nil {
				return nil, err
			}
			out[k] = w
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			w, err := walkAndTemplate(val, vars)
			if err != nil {
				return nil, err
			}
			out[i] = w
		}
		return out, nil
	default:
		return v, nil
	}
}

func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func pathOrRoot(p string) string {
	if p == "" {
		return "<root>"
	}
	return strings.TrimPrefix(p, ".")
}

func setEnv(env map[string]string) func() {
	original := make(map[string]*string, len(env))
	for k, v := range env {
		if cur, ok := os.LookupEnv(k); ok {
			c := cur
			original[k] = &c
		} else {
			original[k] = nil
		}
		_ = os.Setenv(k, v)
	}
	return func() {
		for k, prev := range original {
			if prev == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *prev)
			}
		}
	}
}
