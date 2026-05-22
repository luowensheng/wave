// Tests focus on the security guardrails — path traversal, ignore
// patterns, dir vs file dispatch. Directory-index HTML rendering
// and the prettify HTML pipeline are not re-tested here; they live
// in infra/render.
package file_server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupFS creates a temp dir with a controlled file tree and returns
// (dir, cleanupFn). The tree:
//
//   ./hello.txt      "hello"
//   ./sub/secret.txt "secret"
//   ./_hidden.txt    "hidden"
func setupFS(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644))
	must(os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "sub", "secret.txt"), []byte("secret"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "_hidden.txt"), []byte("hidden"), 0o644))
	return dir
}

func hit(t *testing.T, cfg *Config, mountPath, urlPath string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := cfg.CreateRoute("GET", mountPath, nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", urlPath, nil))
	return rr
}

func TestFS_ServesFile(t *testing.T) {
	dir := setupFS(t)
	rr := hit(t, &Config{Dir: dir}, "/files", "/files/hello.txt")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	if rr.Body.String() != "hello" {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestFS_ServesNestedFile(t *testing.T) {
	dir := setupFS(t)
	rr := hit(t, &Config{Dir: dir}, "/files", "/files/sub/secret.txt")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	if rr.Body.String() != "secret" {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestFS_DirectoryIndex(t *testing.T) {
	dir := setupFS(t)
	rr := hit(t, &Config{Dir: dir}, "/files", "/files/")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type=%q", ct)
	}
	// The HTML index should mention the files in the dir.
	if !strings.Contains(rr.Body.String(), "hello.txt") {
		t.Fatalf("directory index missing hello.txt")
	}
}

func TestFS_NotFoundMissing(t *testing.T) {
	dir := setupFS(t)
	rr := hit(t, &Config{Dir: dir}, "/files", "/files/nope.txt")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

// Path traversal — the canonical attack. With Dir set to a tmp
// directory, no `../`-laden URL should be able to escape it.
func TestFS_BlocksPathTraversal(t *testing.T) {
	dir := setupFS(t)
	// Create a file *outside* the served dir that we should not be
	// able to read.
	parent := filepath.Dir(dir)
	leak := filepath.Join(parent, "leak.txt")
	if err := os.WriteFile(leak, []byte("DO NOT READ"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(leak)

	// Various traversal attempts. They differ in how the request URL
	// looks, but they all aim at ../leak.txt.
	for _, urlPath := range []string{
		"/files/../leak.txt",
		"/files/sub/../../leak.txt",
		"/files/./../leak.txt",
		"/files/sub/../../../" + filepath.Base(leak),
	} {
		rr := hit(t, &Config{Dir: dir}, "/files", urlPath)
		if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "DO NOT READ") {
			t.Fatalf("PATH TRAVERSAL: url=%q leaked file contents", urlPath)
		}
	}
}

func TestFS_FileIgnorePatterns(t *testing.T) {
	dir := setupFS(t)
	cfg := &Config{
		Dir:                dir,
		FileIgnorePatterns: []string{"_hidden", "secret"},
	}
	for _, urlPath := range []string{"/files/_hidden.txt", "/files/sub/secret.txt"} {
		rr := hit(t, cfg, "/files", urlPath)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("url=%q: got %d, want 404 (ignored pattern)", urlPath, rr.Code)
		}
	}
	// hello.txt is not in the ignore list — should still serve.
	rr := hit(t, cfg, "/files", "/files/hello.txt")
	if rr.Code != http.StatusOK {
		t.Fatalf("hello.txt should still be reachable, got %d", rr.Code)
	}
}

func TestFS_PrettifyFallbackOnNonHTMLable(t *testing.T) {
	dir := setupFS(t)
	cfg := &Config{Dir: dir, Prettify: true}
	// hello.txt isn't a code/markdown file — prettify falls through
	// to http.ServeFile, which sets Content-Type from the extension.
	rr := hit(t, cfg, "/files", "/files/hello.txt")
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestFS_ValidateAlwaysPasses(t *testing.T) {
	// Validate is a no-op today; assert that contract for future
	// regressions (this test breaks loudly if Validate gains real
	// checks without updating callers).
	if err := (&Config{}).Validate(); err != nil {
		t.Fatalf("Validate should return nil, got %v", err)
	}
}
