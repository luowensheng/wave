package totp_routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luowensheng/wave/infra/totp"
)

// fakeStore is a tiny in-memory hook impl that mimics what the
// orchestrator wires to the real DB.
type fakeStore struct {
	mu      sync.Mutex
	pending map[string]string
	secrets map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		pending: map[string]string{},
		secrets: map[string]string{},
	}
}

func (f *fakeStore) wire(uid string) {
	PendingPut = func(_ context.Context, u, s string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pending[u] = s
		return nil
	}
	PendingGet = func(_ context.Context, u string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.pending[u], nil
	}
	SecretSet = func(_ context.Context, u, s string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.secrets[u] = s
		delete(f.pending, u)
		return nil
	}
	SecretGet = func(_ context.Context, u string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.secrets[u], nil
	}
	CurrentUserID = func(r *http.Request) string { return uid }
}

func TestEnrollFullFlow(t *testing.T) {
	fs := newFakeStore()
	fs.wire("user-1")

	// 1. Start.
	startH, _ := (&EnrollStartConfig{Issuer: "MyApp"}).CreateRoute("POST", "/", nil)
	w := httptest.NewRecorder()
	startH(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 200 {
		t.Fatalf("start status = %d", w.Code)
	}
	var startResp struct {
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&startResp); err != nil {
		t.Fatal(err)
	}
	if startResp.Secret == "" || !strings.Contains(startResp.OTPAuthURL, "otpauth://") {
		t.Errorf("bad start response: %+v", startResp)
	}

	// 2. Use the secret to compute a valid code; confirm.
	now := time.Now()
	nowFn = func() time.Time { return now }
	defer func() { nowFn = time.Now }()
	code, _ := totp.Generate(startResp.Secret, now)

	confirmH, _ := (&EnrollConfirmConfig{}).CreateRoute("POST", "/", nil)
	body := strings.NewReader(`{"code":"` + code + `"}`)
	w2 := httptest.NewRecorder()
	confirmH(w2, httptest.NewRequest("POST", "/", body))
	if w2.Code != 200 {
		t.Fatalf("confirm status = %d body=%q", w2.Code, w2.Body.String())
	}

	// 3. Standalone verify uses the persisted secret.
	verifyH, _ := (&VerifyConfig{}).CreateRoute("POST", "/", nil)
	body2 := strings.NewReader(`{"code":"` + code + `"}`)
	w3 := httptest.NewRecorder()
	verifyH(w3, httptest.NewRequest("POST", "/", body2))
	if w3.Code != 200 {
		t.Errorf("verify status = %d", w3.Code)
	}
}

func TestEnrollConfirmRejectsWrongCode(t *testing.T) {
	fs := newFakeStore()
	fs.wire("u")
	fs.pending["u"] = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	confirmH, _ := (&EnrollConfirmConfig{}).CreateRoute("POST", "/", nil)
	w := httptest.NewRecorder()
	confirmH(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"code":"000000"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
}

func TestUnauthenticatedRoutesReturn401(t *testing.T) {
	CurrentUserID = func(r *http.Request) string { return "" }
	for _, h := range []http.HandlerFunc{
		mustHandler(&EnrollStartConfig{}),
		mustHandler(&EnrollConfirmConfig{}),
		mustHandler(&VerifyConfig{}),
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d", w.Code)
		}
	}
}

type creator interface {
	CreateRoute(string, string, map[string]string) (http.HandlerFunc, error)
}

func mustHandler(c creator) http.HandlerFunc {
	h, err := c.CreateRoute("POST", "/", nil)
	if err != nil {
		panic(err)
	}
	return h
}

func TestVerifyWithoutEnrollment(t *testing.T) {
	fs := newFakeStore()
	fs.wire("u")
	verifyH, _ := (&VerifyConfig{}).CreateRoute("POST", "/", nil)
	w := httptest.NewRecorder()
	verifyH(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"code":"123456"}`)))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d", w.Code)
	}
}
