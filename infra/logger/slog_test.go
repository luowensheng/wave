package logger

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infrahttp "wave/infra/http"
)

func TestFromRequestPicksUpID(t *testing.T) {
	var buf bytes.Buffer
	old := Slog
	defer func() { Slog = old }()
	Slog = slog.New(slog.NewTextHandler(&buf, nil))

	// Run a request through the real RequestID middleware so the
	// id ends up on r.Context(); the inner handler then logs.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		FromRequest(r).Info("hi")
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-Id", "abc-xyz")
	infrahttp.RequestIDMiddleware(inner).ServeHTTP(httptest.NewRecorder(), r)

	if !strings.Contains(buf.String(), "request_id=abc-xyz") {
		t.Errorf("missing request_id: %s", buf.String())
	}
}

func TestFromRequestNoMiddlewareFallsBack(t *testing.T) {
	var buf bytes.Buffer
	old := Slog
	defer func() { Slog = old }()
	Slog = slog.New(slog.NewTextHandler(&buf, nil))

	r := httptest.NewRequest("GET", "/", nil)
	FromRequest(r).Info("hi")
	if strings.Contains(buf.String(), "request_id") {
		t.Errorf("should not have request_id without middleware: %s", buf.String())
	}
}
