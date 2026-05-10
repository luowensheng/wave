package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func boomHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, body, status)
	}
}

func TestErrorCaseDefaultRangeRedirects4xx5xx(t *testing.T) {
	onFail := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/error", http.StatusFound)
	}
	mw := errorCaseMiddleware(&LimitEntry{Case: CaseError}, onFail)
	for _, code := range []int{400, 404, 500, 503} {
		h := mw(boomHandler(code, "x"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != http.StatusFound {
			t.Errorf("status %d → got %d, want 302", code, w.Code)
		}
	}
}

func TestErrorCasePassesSuccess(t *testing.T) {
	onFail := func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("onFail invoked on success")
	}
	mw := errorCaseMiddleware(&LimitEntry{Case: CaseError}, onFail)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestErrorCaseStatusCodesFilter(t *testing.T) {
	calls := 0
	onFail := func(w http.ResponseWriter, r *http.Request) { calls++ }
	mw := errorCaseMiddleware(&LimitEntry{
		Case:        CaseError,
		StatusCodes: []int{500, 502},
	}, onFail)
	// 500 → fires
	mw(boomHandler(500, "x")).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	// 401 → passes
	w := httptest.NewRecorder()
	mw(boomHandler(401, "y")).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if calls != 1 {
		t.Errorf("calls = %d", calls)
	}
	if w.Code != 401 || !strings.Contains(w.Body.String(), "y") {
		t.Errorf("non-matching status didn't pass: code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestErrorCaseRange(t *testing.T) {
	calls := 0
	onFail := func(w http.ResponseWriter, r *http.Request) { calls++ }
	mw := errorCaseMiddleware(&LimitEntry{
		Case:      CaseError,
		StatusMin: 500, StatusMax: 599,
	}, onFail)
	mw(boomHandler(503, "x")).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	mw(boomHandler(404, "y")).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if calls != 1 {
		t.Errorf("calls = %d (expected 1: only 503 in range)", calls)
	}
}

func TestErrorCaseStreamingOptsOut(t *testing.T) {
	calls := 0
	onFail := func(w http.ResponseWriter, r *http.Request) { calls++ }
	mw := errorCaseMiddleware(&LimitEntry{Case: CaseError}, onFail)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("second"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if calls != 0 {
		t.Errorf("streaming should opt out, calls=%d", calls)
	}
	if w.Code != 500 || w.Body.String() != "firstsecond" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}
