package servers

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// readinessFlag flips to 1 once Start has fully initialized everything.
// /readyz returns 503 until then. /healthz is always 200 — it only proves
// the process is up.
var (
	readinessFlag atomic.Int32
	bootTime      = time.Now()
)

// Version is set at build time via -ldflags "-X wave/orchestrator/server.Version=..."
var Version = "dev"

func registerHealthRoutes(mux *http.ServeMux) {
	// Register as GET-only. Without an explicit method Go 1.22+ ServeMux
	// treats a bare path as "any method" and panics on conflict with
	// sibling routes that ARE method-specific (e.g. a user's `GET /` file
	// route), because the bare pattern matches more methods than they
	// do while having a more specific path. Health endpoints are
	// idempotent reads — GET-only is the right contract anyway.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"uptime_sec": int(time.Since(bootTime).Seconds()),
			"version":    Version,
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if readinessFlag.Load() == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": Version})
	})
}

// markReady is called from Start once the server is fully wired and
// listening. After this /readyz returns 200.
func markReady() {
	readinessFlag.Store(1)
}
