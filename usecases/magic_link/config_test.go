package magic_link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"wave/infra/mailer"
	"wave/infra/verify"
)

func TestMagicLinkRoundTrip(t *testing.T) {
	cap := mailer.NewCaptureSender()
	mailer.SetDefault(cap)
	defer mailer.SetDefault(mailer.NewConsoleSender(nil))

	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	var loggedInAs string
	loggedInCount := atomic.Int32{}
	SetLoginFn(func(_ context.Context, email string, w http.ResponseWriter, r *http.Request) error {
		loggedInAs = email
		loggedInCount.Add(1)
		return nil
	})

	// Step 1: request a magic link.
	reqCfg := &RequestConfig{
		CallbackURL: "http://localhost/login/verify",
		Subject:     "Sign in",
		EmailBody:   "{{.Link}}",
	}
	reqH, _ := reqCfg.CreateRoute("POST", "/login/request", nil)
	w := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"alice@x.io"}`)
	r := httptest.NewRequest("POST", "/login/request", body)
	reqH(w, r)
	if w.Code != 200 {
		t.Fatalf("request status = %d", w.Code)
	}
	msg := cap.LastTo("alice@x.io")
	if msg == nil {
		t.Fatal("no email captured")
	}
	// Extract token from the link in the body.
	const prefix = "http://localhost/login/verify?token="
	if !strings.Contains(msg.TextBody, prefix) {
		t.Fatalf("body didn't include link: %q", msg.TextBody)
	}
	token := strings.TrimPrefix(msg.TextBody, prefix)
	token = strings.TrimSpace(token)
	if token == "" {
		t.Fatal("empty token")
	}

	// Step 2: consume the token.
	conCfg := &ConsumeConfig{}
	conH, _ := conCfg.CreateRoute("GET", "/login/verify", nil)
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/login/verify?token="+token, nil)
	conH(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("consume status = %d body=%q", w2.Code, w2.Body.String())
	}
	if loggedInAs != "alice@x.io" {
		t.Errorf("login fn got %q", loggedInAs)
	}

	// Token is single-use → second consume must fail.
	w3 := httptest.NewRecorder()
	conH(w3, httptest.NewRequest("GET", "/login/verify?token="+token, nil))
	if w3.Code != http.StatusUnauthorized {
		t.Errorf("re-use status = %d", w3.Code)
	}
}

func TestRequestRequiresEmail(t *testing.T) {
	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	cfg := &RequestConfig{CallbackURL: "http://x/v"}
	h, _ := cfg.CreateRoute("POST", "/req", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", "/req", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
}

func TestConsumeMissingTokenRejected(t *testing.T) {
	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	cfg := &ConsumeConfig{}
	h, _ := cfg.CreateRoute("GET", "/c", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/c", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
}

func TestConsumeRedirect(t *testing.T) {
	mailer.SetDefault(mailer.NewCaptureSender())
	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	SetLoginFn(func(_ context.Context, _ string, _ http.ResponseWriter, _ *http.Request) error { return nil })

	tok, _ := issuer.Issue(context.Background(), verify.IssueOpts{Subject: subject, Value: "u@x"})

	cfg := &ConsumeConfig{SuccessRedirect: "/welcome"}
	h, _ := cfg.CreateRoute("GET", "/c", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/c?token="+string(tok), nil))
	if w.Code != http.StatusFound {
		t.Errorf("status = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/welcome" {
		t.Errorf("location = %q", loc)
	}
}

// keep imports tidy
var _ = json.Marshal

func TestMagicLinkLoadsBodyFromFile(t *testing.T) {
	cap := mailer.NewCaptureSender()
	mailer.SetDefault(cap)
	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	SetLoginFn(func(_ context.Context, _ string, _ http.ResponseWriter, _ *http.Request) error { return nil })

	// Drop a template file; the request handler should pick it up.
	dir := t.TempDir()
	textPath := dir + "/email.txt"
	htmlPath := dir + "/email.html"
	if err := os.WriteFile(textPath, []byte("Token link: {{.Link}} (file-loaded)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte("<p>Click <a href='{{.Link}}'>here</a></p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &RequestConfig{
		CallbackURL:       "http://localhost/login/verify",
		EmailBodyFile:     textPath,
		EmailBodyHTMLFile: htmlPath,
	}
	h, err := cfg.CreateRoute("POST", "/req", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", "/req", strings.NewReader(`{"email":"alice@x.io"}`)))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	msg := cap.LastTo("alice@x.io")
	if msg == nil {
		t.Fatal("no email captured")
	}
	if !strings.Contains(msg.TextBody, "file-loaded") {
		t.Errorf("plain body did not come from file: %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<a href=") {
		t.Errorf("html body did not come from file: %q", msg.HTMLBody)
	}
	if !strings.Contains(msg.TextBody, "?token=") {
		t.Errorf("template not rendered: %q", msg.TextBody)
	}
}

func TestMagicLinkInlineBodyTakesPrecedenceOverFile(t *testing.T) {
	cap := mailer.NewCaptureSender()
	mailer.SetDefault(cap)
	SetIssuer(verify.NewIssuer(verify.NewMemoryStore(), []byte("k")))
	SetLoginFn(func(_ context.Context, _ string, _ http.ResponseWriter, _ *http.Request) error { return nil })

	dir := t.TempDir()
	path := dir + "/wrong.txt"
	_ = os.WriteFile(path, []byte("FROM_FILE"), 0o644)

	cfg := &RequestConfig{
		CallbackURL:   "http://localhost/v",
		EmailBody:     "FROM_INLINE {{.Link}}",
		EmailBodyFile: path,
	}
	h, _ := cfg.CreateRoute("POST", "/r", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", "/r", strings.NewReader(`{"email":"a@x"}`)))

	msg := cap.LastTo("a@x")
	if msg == nil || !strings.Contains(msg.TextBody, "FROM_INLINE") {
		t.Errorf("inline body not used: %+v", msg)
	}
	if msg != nil && strings.Contains(msg.TextBody, "FROM_FILE") {
		t.Errorf("file body leaked despite inline: %q", msg.TextBody)
	}
}

func TestMagicLinkMissingFileFailsAtBoot(t *testing.T) {
	cfg := &RequestConfig{
		CallbackURL:   "http://localhost/v",
		EmailBodyFile: "/nonexistent/never.txt",
	}
	if _, err := cfg.CreateRoute("POST", "/r", nil); err == nil {
		t.Error("expected error for missing template file")
	}
}
