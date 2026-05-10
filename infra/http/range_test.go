package http

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, body []byte, header string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	if header != "" {
		r.Header.Set("Range", header)
	}
	w := httptest.NewRecorder()
	err := ServeRange(w, r, bytes.NewReader(body), int64(len(body)), "application/octet-stream")
	return w, err
}

func TestServeRangeNoHeaderReturnsFull(t *testing.T) {
	body := []byte("hello world")
	w, err := serve(t, body, "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if w.Body.String() != string(body) {
		t.Errorf("body = %q", w.Body.String())
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Errorf("missing Accept-Ranges")
	}
}

func TestServeRangePrefix(t *testing.T) {
	w, err := serve(t, []byte("abcdefghij"), "bytes=2-5")
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusPartialContent {
		t.Errorf("status = %d", w.Code)
	}
	if w.Body.String() != "cdef" {
		t.Errorf("body = %q", w.Body.String())
	}
	if cr := w.Header().Get("Content-Range"); cr != "bytes 2-5/10" {
		t.Errorf("Content-Range = %q", cr)
	}
}

func TestServeRangeOpenEnded(t *testing.T) {
	w, err := serve(t, []byte("0123456789"), "bytes=7-")
	if err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != "789" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestServeRangeSuffix(t *testing.T) {
	w, err := serve(t, []byte("abcdefghij"), "bytes=-3")
	if err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != "hij" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestServeRangeMalformed(t *testing.T) {
	w, _ := serve(t, []byte("abc"), "items=0-1")
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d", w.Code)
	}
}

func TestServeRangeStartBeyondEnd(t *testing.T) {
	w, _ := serve(t, []byte("abc"), "bytes=100-200")
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d", w.Code)
	}
}

func TestServeRangeMultiRangeRefused(t *testing.T) {
	w, _ := serve(t, []byte("abcdef"), "bytes=0-1,3-4")
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("status = %d", w.Code)
	}
}

// Integration: feed through an httptest server so we exercise net/http.
func TestServeRangeOverHTTP(t *testing.T) {
	body := []byte(strings.Repeat("x", 4096))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ServeRange(w, r, bytes.NewReader(body), int64(len(body)), "application/octet-stream")
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=10-19")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || len(got) != 10 {
		t.Errorf("status=%d len=%d", resp.StatusCode, len(got))
	}
}
