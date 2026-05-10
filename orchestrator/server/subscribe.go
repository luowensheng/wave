package servers

import (
	"log"
	"net/http"

	"wave/infra/connections"
	"wave/orchestrator/features/auth"
)

// registerSubscribeRoutes registers a GET handler at every configured
// connection's `subscribe_path`. The handler:
//   - applies CORS preflight + allow-origin headers
//   - applies subscribe_auth (if any) using the same auth.RequireAuth
//     middleware that protects normal routes
//   - opens an SSE stream backed by the broker
//
// ws/auto types fall back to SSE in this build.
func (s *Server) registerSubscribeRoutes() {
	reg := connections.Default()
	if reg == nil {
		return
	}
	for name, broker := range reg.All() {
		cfg := broker.Config()
		path := cfg.SubscribePath
		if path == "" {
			continue
		}

		base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if connections.HandleCORS(w, r, cfg.SubscribeCorsOrigins) && r.Method == http.MethodOptions {
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// Dispatcher: type=ws → WebSocket, type=auto → upgrade-aware,
			// otherwise SSE. WebSocket transport is implemented in pure
			// stdlib (see infra/connections/ws.go).
			connections.Serve(w, r, broker)
		})

		var handler http.Handler = base
		if len(cfg.SubscribeAuth) > 0 {
			handler = auth.RequireAuth(handler, cfg.SubscribeAuth...)
		}

		s.mux.Handle(path, handler)
		log.Printf("registered subscribe route: GET %s (connection=%s, auth=%v, cors=%v)",
			path, name, cfg.SubscribeAuth, cfg.SubscribeCorsOrigins)
	}
}
