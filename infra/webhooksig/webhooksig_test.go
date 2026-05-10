package webhooksig

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func sign(body, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func TestStripeOK(t *testing.T) {
	body := `{"id":"evt_1"}`
	ts := time.Now().Unix()
	tsStr := strconv.FormatInt(ts, 10)
	m := hmac.New(sha256.New, []byte("whsec_x"))
	m.Write([]byte(tsStr + "." + body))
	v1 := hex.EncodeToString(m.Sum(nil))

	v, err := New(Config{Provider: ProviderStripe, Secret: "whsec_x"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	r.Header.Set("Stripe-Signature", "t="+tsStr+",v1="+v1)
	if err := v.Verify(r); err != nil {
		t.Errorf("verify: %v", err)
	}
	// Body must still be readable downstream.
	got, _ := io.ReadAll(r.Body)
	if string(got) != body {
		t.Errorf("body changed: %q", got)
	}
}

func TestStripeRejectsOldTimestamp(t *testing.T) {
	body := `{}`
	ts := time.Now().Add(-time.Hour).Unix()
	tsStr := strconv.FormatInt(ts, 10)
	m := hmac.New(sha256.New, []byte("k"))
	m.Write([]byte(tsStr + "." + body))
	v1 := hex.EncodeToString(m.Sum(nil))

	v, _ := New(Config{Provider: ProviderStripe, Secret: "k", Tolerance: 5 * time.Minute})
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	r.Header.Set("Stripe-Signature", "t="+tsStr+",v1="+v1)
	if err := v.Verify(r); err == nil {
		t.Error("expected age error")
	}
}

func TestStripeBadSig(t *testing.T) {
	v, _ := New(Config{Provider: ProviderStripe, Secret: "k"})
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("hi")))
	r.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=deadbeef", time.Now().Unix()))
	if err := v.Verify(r); err == nil {
		t.Error("expected mismatch")
	}
}

func TestGitHubOK(t *testing.T) {
	body := `{"action":"push"}`
	want := "sha256=" + sign(body, "secret")
	v, _ := New(Config{Provider: ProviderGitHub, Secret: "secret"})
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	r.Header.Set("X-Hub-Signature-256", want)
	if err := v.Verify(r); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestSlackOK(t *testing.T) {
	body := "token=x"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	m := hmac.New(sha256.New, []byte("s8B0bf4"))
	fmt.Fprintf(m, "v0:%s:", ts)
	m.Write([]byte(body))
	sig := "v0=" + hex.EncodeToString(m.Sum(nil))

	v, _ := New(Config{Provider: ProviderSlack, Secret: "s8B0bf4"})
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	r.Header.Set("X-Slack-Signature", sig)
	r.Header.Set("X-Slack-Request-Timestamp", ts)
	if err := v.Verify(r); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestGenericWithPrefix(t *testing.T) {
	body := "abc"
	v, _ := New(Config{
		Provider: ProviderGeneric, Secret: "k",
		Header: "X-Sig", HeaderPrefix: "sha256=",
	})
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	r.Header.Set("X-Sig", "sha256="+sign(body, "k"))
	if err := v.Verify(r); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestMiddlewareRejects401(t *testing.T) {
	v, _ := New(Config{Provider: ProviderGitHub, Secret: "k"})
	mw := Middleware(v)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("body")))
	// no header → must fail
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
}

func TestNewRejectsEmptySecret(t *testing.T) {
	if _, err := New(Config{Provider: ProviderStripe}); err == nil {
		t.Error("expected error")
	}
}
