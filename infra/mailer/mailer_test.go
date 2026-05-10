package mailer

import (
	"bytes"
	"strings"
	"testing"
)

func TestCaptureSenderRecords(t *testing.T) {
	c := NewCaptureSender()
	_ = c.Send(Message{To: []string{"a@x"}, Subject: "hi", TextBody: "hello"})
	_ = c.Send(Message{To: []string{"b@x"}, Subject: "yo", TextBody: "hey"})
	if len(c.Messages) != 2 {
		t.Fatalf("got %d", len(c.Messages))
	}
	if got := c.LastTo("a@x"); got == nil || got.Subject != "hi" {
		t.Errorf("LastTo a@x = %+v", got)
	}
	if c.LastTo("c@x") != nil {
		t.Error("LastTo for unknown address should be nil")
	}
}

func TestConsoleSenderWritesOutput(t *testing.T) {
	var buf bytes.Buffer
	c := NewConsoleSender(&buf)
	_ = c.Send(Message{To: []string{"x@y"}, Subject: "hello", TextBody: "world"})
	out := buf.String()
	for _, want := range []string{"x@y", "hello", "world"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

func TestRenderTextAndHTML(t *testing.T) {
	got, err := RenderText("hi {{.Name}}, code is {{.Code}}", map[string]string{"Name": "Alice", "Code": "42"})
	if err != nil || got != "hi Alice, code is 42" {
		t.Errorf("text render: %q err=%v", got, err)
	}
	gotH, err := RenderHTML("<p>Hello {{.Name}}</p>", map[string]string{"Name": "<b>Alice</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotH, "&lt;b&gt;Alice") {
		t.Errorf("HTML render didn't escape: %q", gotH)
	}
}

func TestBuildMIMEMultipart(t *testing.T) {
	body := buildMIME(Message{
		To:       []string{"a@x"},
		Subject:  "test",
		TextBody: "plain",
		HTMLBody: "<b>html</b>",
	}, "from@y")
	s := string(body)
	for _, want := range []string{"From: from@y", "To: a@x", "Subject: test",
		"multipart/alternative", "Content-Type: text/plain", "Content-Type: text/html",
		"plain", "<b>html</b>"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in MIME body", want)
		}
	}
}

func TestBuildMIMEPlainOnly(t *testing.T) {
	body := buildMIME(Message{To: []string{"a@x"}, Subject: "s", TextBody: "p"}, "f@y")
	s := string(body)
	if strings.Contains(s, "multipart") {
		t.Errorf("plain-only should not be multipart: %s", s)
	}
	if !strings.Contains(s, "Content-Type: text/plain") {
		t.Errorf("missing text/plain: %s", s)
	}
}

func TestNewSMTPSenderRequiresFields(t *testing.T) {
	if _, err := NewSMTPSender(SMTPConfig{}); err == nil {
		t.Error("expected error for empty config")
	}
	if _, err := NewSMTPSender(SMTPConfig{Host: "x", Port: 25}); err == nil {
		t.Error("expected error for missing From")
	}
}
