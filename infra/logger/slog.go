package logger

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	infrahttp "wave/infra/http"
)

// Slog is the structured logger handle. Defaults to a text handler at
// info level. Set WAVE_LOG_FORMAT=json for JSON, =text (default)
// for human, and WAVE_LOG_LEVEL=debug|info|warn|error.
//
// This is *additive* to the existing colored access logger — call sites
// can adopt slog gradually without breaking the dev-friendly request log.
var Slog = newSlog()

func newSlog() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("WAVE_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(os.Getenv("WAVE_LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

// FromRequest returns a logger pre-bound with the request ID set by
// infra/http.RequestIDMiddleware. Falls back to the global Slog when the
// middleware is not in the chain.
func FromRequest(r *http.Request) *slog.Logger {
	if id := infrahttp.RequestIDFrom(r.Context()); id != "" {
		return Slog.With("request_id", id)
	}
	return Slog
}

// FromContext is the context-only variant for handlers that already
// extracted the context.
func FromContext(ctx context.Context) *slog.Logger {
	if id := infrahttp.RequestIDFrom(ctx); id != "" {
		return Slog.With("request_id", id)
	}
	return Slog
}
