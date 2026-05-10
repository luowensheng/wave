package servers

import (
	"encoding/json"
	"net/http"

	"wave/infra/connections"
)

// StreamRoute is a frontend-discoverable description of a streaming
// endpoint: which broker subscribers should connect to, and what the
// publisher will tag events as.
type StreamRoute struct {
	RouteID       string `json:"route_id"`
	PublishPath   string `json:"publish_path"`
	PublishMethod string `json:"publish_method"`
	Connection    string `json:"connection"`
	SubscribePath string `json:"subscribe_path"`
	EventType     string `json:"event_type,omitempty"`
}

// registerStreamDiscovery installs GET /api/streams.json. Frontends fetch
// this once at boot and look up `route_id` to find the right SSE endpoint
// instead of hardcoding paths.
func (s *Server) registerStreamDiscovery() {
	if s.Config == nil {
		return
	}

	streams := []StreamRoute{}
	creg := connections.Default()
	for _, route := range s.Config.Routes {
		if route.Type != "stream-publish" || route.StreamPublishConfig == nil {
			continue
		}
		sp := route.StreamPublishConfig
		entry := StreamRoute{
			RouteID:       sp.RouteID,
			PublishPath:   route.Path,
			PublishMethod: route.Method,
			Connection:    sp.Connection,
			EventType:     sp.EventType,
		}
		if creg != nil {
			if b, ok := creg.Get(sp.Connection); ok {
				entry.SubscribePath = b.Config().SubscribePath
			}
		}
		streams = append(streams, entry)
	}

	if len(streams) == 0 {
		return
	}

	body, _ := json.Marshal(streams)
	s.mux.HandleFunc("GET /api/streams.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}
