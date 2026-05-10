package studio

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed all:web
var webFS embed.FS

// Serve boots the studio HTTP server. Blocks until SIGINT/SIGTERM.
func Serve(host string, port int, dataDir string, openBrowser bool) error {
	dataDir = expandUser(dataDir)
	reg, err := LoadRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("studio: registry: %w", err)
	}
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("studio: cannot locate self executable: %w", err)
	}
	sup := NewSupervisor(binary)
	token, err := loadOrCreateToken(dataDir)
	if err != nil {
		return fmt.Errorf("studio: token: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", authMiddleware(token, apiHandler(reg, sup)))
	mux.Handle("/", staticHandler())

	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{Addr: addr, Handler: mux}

	url := fmt.Sprintf("http://%s/?t=%s", addr, token)
	fmt.Fprintf(os.Stderr, "Studio running at %s\n", url)
	fmt.Fprintf(os.Stderr, "  data-dir: %s\n", dataDir)
	fmt.Fprintf(os.Stderr, "  binary:   %s\n", binary)

	if openBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			openInBrowser(url)
		}()
	}

	// graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Fprintln(os.Stderr, "studio: shutting down…")
		sup.StopAll()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// staticHandler serves the embedded web/ directory. Falls back to
// /index.html for client-side routes (anything that doesn't match a
// real file).
func staticHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("studio: web embed: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the requested path; if it 404s, serve index.html.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func expandUser(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// openInBrowser fires off the OS-specific "open URL" command. Best
// effort — failures are silent (the URL is already printed to stderr).
func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
