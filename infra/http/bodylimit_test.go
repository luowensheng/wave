package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBodyLimitDefault413WithJSON(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{MaxBytes: 8})
	called := atomic.Int32{}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1) }))

	r := httptest.NewRequest("POST", "/", strings.NewReader("oversize-body"))
	r.ContentLength = 13
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"limit_bytes":8`) {
		t.Errorf("body = %q", w.Body.String())
	}
	if called.Load() != 0 {
		t.Error("inner handler should not run")
	}
}

func TestBodyLimitRedirect(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{MaxBytes: 4, Redirect: "/too-big"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("toolarge"))
	r.ContentLength = 8
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Errorf("status = %d", w.Code)
	}
	if w.Header().Get("Location") != "/too-big" {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
}

func TestBodyLimitInlineTemplate(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{
		MaxBytes: 4, TemplateInline: "<h1>too big!</h1>",
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("toolarge"))
	r.ContentLength = 8
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<h1>too big!</h1>") {
		t.Errorf("body = %q", w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestBodyLimitFileTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "413.txt")
	_ = os.WriteFile(path, []byte("plain text body too big"), 0o644)
	mw := BodyLimitMiddleware(BodyLimitConfig{MaxBytes: 4, TemplateFile: path})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("toolarge"))
	r.ContentLength = 8
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "plain text") {
		t.Errorf("body = %q", w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestBodyLimitStatusOverride(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{
		MaxBytes: 4, StatusOverride: 400,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("oversize"))
	r.ContentLength = 8
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestBodyLimitOnExceededHook(t *testing.T) {
	hits := atomic.Int32{}
	mw := BodyLimitMiddleware(BodyLimitConfig{
		MaxBytes: 4,
		OnExceeded: func(_ *http.Request, _ int64) { hits.Add(1) },
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("oversize"))
	r.ContentLength = 8
	h.ServeHTTP(httptest.NewRecorder(), r)
	if hits.Load() != 1 {
		t.Errorf("hook hits = %d", hits.Load())
	}
}

func TestBodyLimitWithinLimitPasses(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{MaxBytes: 100})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("small"))
	r.ContentLength = 5
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestBodyLimitZeroMaxBytesDisables(t *testing.T) {
	mw := BodyLimitMiddleware(BodyLimitConfig{MaxBytes: 0})
	called := atomic.Int32{}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1) }))
	r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 1000000)))
	r.ContentLength = 1000000
	h.ServeHTTP(httptest.NewRecorder(), r)
	if called.Load() != 1 {
		t.Error("zero limit should disable, but inner handler was skipped")
	}
}

func TestParseBytesString(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"5MB", 5 << 20},
		{"5mb", 5 << 20},
		{"1.5gb", 1610612736},
		{"200KB", 200 << 10},
		{"1024", 1024},
		{"1G", 1 << 30},
		{"1tb", 1 << 40},
	}
	for _, c := range cases {
		got, err := ParseBytesString(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseBytesString(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "-5MB", "MB"} {
		if _, err := ParseBytesString(bad); err == nil {
			t.Errorf("ParseBytesString(%q) should fail", bad)
		}
	}
}
