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
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"uptime_sec": int(time.Since(bootTime).Seconds()),
			"version":    Version,
		})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if readinessFlag.Load() == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": Version})
	})
}

// markReady is called from Start once the server is fully wired and
// listening. After this /readyz returns 200.
func markReady() {
	readinessFlag.Store(1)
}
