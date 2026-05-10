package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func handler200(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	})
}

func TestETagSetOnGet(t *testing.T) {
	h := ETagMiddleware(handler200("hello"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Header().Get("ETag") == "" {
		t.Error("ETag missing")
	}
	if w.Body.String() != "hello" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestETag304OnMatch(t *testing.T) {
	h := ETagMiddleware(handler200("hello"))

	// First request: capture the ETag.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest("GET", "/", nil))
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response missing ETag")
	}

	// Second request: include If-None-Match → 304.
	w2 := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", etag)
	h.ServeHTTP(w2, r)
	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("304 body = %q", w2.Body.String())
	}
}

func TestETagSkipsMutation(t *testing.T) {
	h := ETagMiddleware(handler200("hello"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Header().Get("ETag") != "" {
		t.Error("ETag should not be set on POST")
	}
}

func TestETagWildcardMatch(t *testing.T) {
	h := ETagMiddleware(handler200("hello"))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Errorf("status = %d", w.Code)
	}
}

func TestETagSkipsStreaming(t *testing.T) {
	streamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: hi\n\n"))
		w.(http.Flusher).Flush()
	})
	h := ETagMiddleware(streamHandler)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Header().Get("ETag") != "" {
		t.Error("streaming response should not get ETag")
	}
	if w.Body.String() != "data: hi\n\n" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestETagWeakValidatorAccepted(t *testing.T) {
	h := ETagMiddleware(handler200("hello"))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest("GET", "/", nil))
	etag := w1.Header().Get("ETag")

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", "W/"+etag)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Errorf("W/ prefix not honored: status = %d", w.Code)
	}
}
