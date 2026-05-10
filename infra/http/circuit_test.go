package http

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func failHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", status)
	})
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
}

func TestCircuitOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour)
	h := cb.Middleware(failHandler(503))
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	}
	if cb.State() != "open" {
		t.Fatalf("state = %q after threshold", cb.State())
	}
	// Next request short-circuits — upstream not called.
	called := atomic.Int64{}
	h2 := cb.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(200)
	}))
	w := httptest.NewRecorder()
	h2.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if called.Load() != 0 {
		t.Errorf("upstream called while open")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status while open = %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("Retry-After missing")
	}
}

func TestCircuitHalfOpenSuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(2, 30*time.Millisecond)
	h := cb.Middleware(failHandler(500))
	// Trip.
	for i := 0; i < 2; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}
	if cb.State() != "open" {
		t.Fatal("not open")
	}
	time.Sleep(50 * time.Millisecond)

	// Now an OK probe should transition closed.
	h2 := cb.Middleware(okHandler())
	w := httptest.NewRecorder()
	h2.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Errorf("probe status = %d", w.Code)
	}
	if cb.State() != "closed" {
		t.Errorf("state after success = %q", cb.State())
	}
}

func TestCircuitHalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(2, 20*time.Millisecond)
	h := cb.Middleware(failHandler(500))
	for i := 0; i < 2; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}
	openedAt := cb.openedAt.Load()
	time.Sleep(40 * time.Millisecond)

	// Probe (still failing) → re-open with fresh cooldown.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if cb.State() != "open" {
		t.Errorf("state = %q after probe failure", cb.State())
	}
	if cb.openedAt.Load() <= openedAt {
		t.Errorf("openedAt should advance on re-open")
	}
}

func TestCircuitConcurrentProbesRejected(t *testing.T) {
	cb := NewCircuitBreaker(2, 20*time.Millisecond)
	for i := 0; i < 2; i++ {
		cb.Middleware(failHandler(500)).ServeHTTP(
			httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}
	time.Sleep(40 * time.Millisecond)

	// Slow probe handler — give second concurrent request time to land
	// on the breaker while first is in flight.
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	})
	h := cb.Middleware(slow)

	var wg sync.WaitGroup
	statuses := make([]int, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			statuses[i] = w.Code
		}(i)
	}
	wg.Wait()

	ok, rejected := 0, 0
	for _, s := range statuses {
		switch s {
		case 200:
			ok++
		case 503:
			rejected++
		}
	}
	if ok != 1 {
		t.Errorf("expected exactly 1 successful probe, got %d", ok)
	}
	if rejected < 1 {
		t.Errorf("expected concurrent probes to be rejected, got %d", rejected)
	}
}

func TestCircuit2xxResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour)
	hFail := cb.Middleware(failHandler(500))
	hOK := cb.Middleware(okHandler())

	hFail.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	hFail.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	hOK.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	// One more failure should NOT trip — counter was reset by the OK.
	hFail.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if cb.State() == "open" {
		t.Error("breaker tripped despite reset")
	}
}
