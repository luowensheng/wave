package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompositionExample_E2E loads the worked examples/composition tree
// through the real NewServer path and asserts the merged+prefixed
// route table, single deduped resources, and the library-as-server
// rejection — the same things `wave routes/serve/validate` exercise.
func TestCompositionExample_E2E(t *testing.T) {
	base, err := filepath.Abs(filepath.Join("..", "..", "examples", "composition"))
	if err != nil {
		t.Fatal(err)
	}
	// NewServer os.Chdir's into the config dir; restore CWD after.
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// Library file refuses to boot as a server.
	if _, err := NewServer(filepath.Join(base, "shared", "app-db.yaml")); err == nil ||
		!strings.Contains(err.Error(), "is a kind:storage library, not a server") {
		t.Fatalf("expected library-as-server rejection, got %v", err)
	}

	// Host app composes both modules.
	srv, err := NewServer(filepath.Join(base, "app.yaml"))
	if err != nil {
		t.Fatalf("NewServer(app.yaml): %v", err)
	}
	cfg := srv.Config

	// One deduped db / gemini / feed.
	if len(cfg.Storage) != 1 || cfg.Storage["db"] == nil {
		t.Fatalf("expected one db, got %+v", cfg.Storage)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins["gemini"] == nil {
		t.Fatalf("expected one gemini plugin, got %+v", cfg.Plugins)
	}
	if cfg.Connections["feed"] == nil || cfg.Connections["feed"].SubscribePath != "/events/feed" {
		t.Fatalf("borrowed feed connection wrong: %+v", cfg.Connections["feed"])
	}

	rows, err := srv.RouteSummaries()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Path] = true
	}
	for _, want := range []string{"/login", "/oauth/callback", "/rss/feeds"} {
		if !got[want] {
			t.Errorf("missing expected route %q (have %v)", want, got)
		}
	}
	if got["/rss/oauth/callback"] {
		t.Error("absolute:true /oauth/callback was incorrectly prefixed")
	}
	if got["/feeds"] {
		t.Error("/feeds should have been prefixed to /rss/feeds")
	}
}
