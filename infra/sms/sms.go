// Package sms is a pluggable SMS sender. Same shape as infra/mailer:
// one interface, three implementations (Twilio, console, capture).
//
// Twilio is included because it's the most common provider and its API
// is small enough to ship a real client; other providers (Plivo,
// Sinch, Vonage, MessageBird, AWS SNS) are easy to add by implementing
// Sender against their REST surface.
package sms

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Message is the payload Senders deliver.
type Message struct {
	From      string // sender phone (E.164) or short-code, depending on provider
	To        string // recipient phone (E.164)
	Body      string
	CreatedAt time.Time
}

// Sender persists / forwards a Message.
type Sender interface {
	Send(m Message) error
}

// ── default global sender ───────────────────────────────────────────

var (
	mu  sync.RWMutex
	def Sender = NewConsoleSender(os.Stderr)
)

func SetDefault(s Sender) { mu.Lock(); defer mu.Unlock(); def = s }
func Default() Sender     { mu.RLock(); defer mu.RUnlock(); return def }

func Send(m Message) error {
	if s := Default(); s != nil {
		if m.CreatedAt.IsZero() {
			m.CreatedAt = time.Now()
		}
		return s.Send(m)
	}
	return fmt.Errorf("sms: no default sender configured")
}

// ── Twilio sender ───────────────────────────────────────────────────

// TwilioConfig holds credentials. Endpoint is overridable so tests can
// point it at httptest.
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
	From       string // either a Twilio number (+15551234567) or messaging-service SID
	Endpoint   string // override base URL; default https://api.twilio.com
	HTTPClient *http.Client
}

// TwilioSender talks to Twilio's REST API.
type TwilioSender struct {
	cfg    TwilioConfig
	client *http.Client
}

func NewTwilioSender(cfg TwilioConfig) (*TwilioSender, error) {
	if cfg.AccountSID == "" || cfg.AuthToken == "" || cfg.From == "" {
		return nil, fmt.Errorf("sms: twilio requires account_sid, auth_token, from")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.twilio.com"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &TwilioSender{cfg: cfg, client: cfg.HTTPClient}, nil
}

func (t *TwilioSender) Send(m Message) error {
	if m.To == "" || m.Body == "" {
		return fmt.Errorf("sms: To and Body required")
	}
	from := m.From
	if from == "" {
		from = t.cfg.From
	}
	form := url.Values{}
	form.Set("To", m.To)
	form.Set("Body", m.Body)
	// Twilio accepts either From=<number> or MessagingServiceSid=<sid>.
	if strings.HasPrefix(from, "MG") && len(from) == 34 {
		form.Set("MessagingServiceSid", from)
	} else {
		form.Set("From", from)
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		strings.TrimRight(t.cfg.Endpoint, "/"), t.cfg.AccountSID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.cfg.AccountSID, t.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("twilio: status %d body=%s", resp.StatusCode, string(body))
}

// ── ConsoleSender (dev) ─────────────────────────────────────────────

type ConsoleSender struct {
	w  io.Writer
	mu sync.Mutex
}

func NewConsoleSender(w io.Writer) *ConsoleSender { return &ConsoleSender{w: w} }

func (c *ConsoleSender) Send(m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.w, "─── sms ───────────────────────────────────────────\n")
	fmt.Fprintf(c.w, "  From: %s\n  To:   %s\n  Body: %s\n",
		m.From, m.To, m.Body)
	fmt.Fprintf(c.w, "──────────────────────────────────────────────────\n")
	return nil
}

// ── CaptureSender (tests) ───────────────────────────────────────────

type CaptureSender struct {
	mu       sync.Mutex
	Messages []Message
}

func NewCaptureSender() *CaptureSender { return &CaptureSender{} }

func (c *CaptureSender) Send(m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Messages = append(c.Messages, m)
	return nil
}

func (c *CaptureSender) LastTo(to string) *Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].To == to {
			return &c.Messages[i]
		}
	}
	return nil
}
