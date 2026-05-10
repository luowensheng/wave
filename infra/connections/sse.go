package connections

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

var subscriberSeq atomic.Int64

// ServeSSE upgrades the request to a Server-Sent Events stream and pumps
// every event from the broker until the client disconnects or the request
// context is cancelled. Heartbeat comments keep middleboxes from idle-
// timing out the long-lived connection.
//
// Pre-formatted event frames go in (see usecases/stream_publish), so
// this handler is transport-only — no formatting policy.
func ServeSSE(w http.ResponseWriter, r *http.Request, broker *Broker) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering

	id := strconv.FormatInt(subscriberSeq.Add(1), 10)
	ch, cancel, ok := broker.Subscribe(id)
	if !ok {
		http.Error(w, "max clients reached", http.StatusServiceUnavailable)
		return
	}
	defer cancel()

	// Initial comment so EventSource transitions to OPEN even before any event.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(broker.cfg.keepAliveDuration())
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// HandleCORS writes the configured CORS allow-origin headers if the
// request Origin matches one of the allowed origins (or "*" is allowed).
// Returns true if the request was a preflight OPTIONS that was answered.
func HandleCORS(w http.ResponseWriter, r *http.Request, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	matched := ""
	for _, a := range allowed {
		if a == "*" || a == origin {
			matched = a
			break
		}
	}
	if matched == "" {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", matched)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Last-Event-ID")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}
