package sms

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCaptureRecords(t *testing.T) {
	c := NewCaptureSender()
	_ = c.Send(Message{To: "+1", Body: "hi"})
	if len(c.Messages) != 1 || c.LastTo("+1").Body != "hi" {
		t.Fatalf("got %+v", c.Messages)
	}
}

func TestConsoleSenderWrites(t *testing.T) {
	var buf bytes.Buffer
	c := NewConsoleSender(&buf)
	_ = c.Send(Message{From: "+1", To: "+2", Body: "hello"})
	for _, want := range []string{"+1", "+2", "hello"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q: %s", want, buf.String())
		}
	}
}

func TestTwilioSendsExpectedRequest(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Verify Basic auth, method, path, content-type, form fields.
		user, pass, ok := r.BasicAuth()
		if !ok || user != "AC123" || pass != "tok" {
			t.Errorf("auth = %v %q %q", ok, user, pass)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/2010-04-01/Accounts/AC123/Messages.json" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("To") != "+15551112222" {
			t.Errorf("To = %q", r.PostForm.Get("To"))
		}
		if r.PostForm.Get("Body") != "hello world" {
			t.Errorf("Body = %q", r.PostForm.Get("Body"))
		}
		if r.PostForm.Get("From") != "+15550000000" {
			t.Errorf("From = %q", r.PostForm.Get("From"))
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"sid":"SM1"}`))
	}))
	defer srv.Close()

	s, err := NewTwilioSender(TwilioConfig{
		AccountSID: "AC123", AuthToken: "tok",
		From: "+15550000000", Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(Message{To: "+15551112222", Body: "hello world"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestTwilioMessagingServiceSidRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		// Long MG-prefixed value should map to MessagingServiceSid, not From.
		if r.PostForm.Get("MessagingServiceSid") != "MG12345678901234567890123456789012" {
			t.Errorf("MessagingServiceSid = %q", r.PostForm.Get("MessagingServiceSid"))
		}
		if r.PostForm.Get("From") != "" {
			t.Errorf("From should be empty, got %q", r.PostForm.Get("From"))
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()
	s, _ := NewTwilioSender(TwilioConfig{
		AccountSID: "AC1", AuthToken: "t",
		From: "MG12345678901234567890123456789012", Endpoint: srv.URL,
	})
	_ = s.Send(Message{To: "+1", Body: "x"})
}

func TestTwilioErrorPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":21211,"message":"Invalid 'To' Phone Number"}`, 400)
	}))
	defer srv.Close()
	s, _ := NewTwilioSender(TwilioConfig{
		AccountSID: "AC1", AuthToken: "t", From: "+1", Endpoint: srv.URL,
	})
	if err := s.Send(Message{To: "+99999", Body: "x"}); err == nil {
		t.Error("expected error on 400")
	}
}

func TestTwilioRejectsEmptyConfig(t *testing.T) {
	if _, err := NewTwilioSender(TwilioConfig{}); err == nil {
		t.Error("expected error")
	}
}
