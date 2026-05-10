// Package mailer is a pluggable email sender. The Sender interface is
// the lowest common denominator (To, Subject, plain + HTML body). Three
// implementations ship out of the box:
//
//   - SMTPSender:    real SMTP via net/smtp; production
//   - ConsoleSender: prints messages to stderr; useful in dev so the
//                    rest of the verification / magic-link / password-
//                    reset machinery works end-to-end without a real
//                    mail server
//   - CaptureSender: records every send into memory for unit tests
//
// Templates: callers pass a struct + template; mailer renders it. Keeps
// every flow's wording in one place and easily testable.
package mailer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"net/smtp"
	"os"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"
)

// Message is the payload Senders deliver.
type Message struct {
	From      string
	To        []string
	Cc        []string
	Bcc       []string
	Subject   string
	TextBody  string
	HTMLBody  string
	Headers   map[string]string
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

// Send is the convenience wrapper most call sites use.
func Send(m Message) error {
	if s := Default(); s != nil {
		if m.CreatedAt.IsZero() {
			m.CreatedAt = time.Now()
		}
		return s.Send(m)
	}
	return fmt.Errorf("mailer: no default sender configured")
}

// ── SMTP sender ─────────────────────────────────────────────────────

// SMTPConfig describes a real outbound SMTP server.
type SMTPConfig struct {
	Host     string // e.g. smtp.sendgrid.net
	Port     int    // 587 (STARTTLS) or 465 (implicit TLS)
	Username string
	Password string
	From     string // default From; can be overridden per-message
	UseTLS   bool   // implicit TLS (port 465)
}

// SMTPSender talks to a real SMTP server.
type SMTPSender struct{ cfg SMTPConfig }

// NewSMTPSender validates basic config and returns a sender. Connection
// is established per-message — keeps sender stateless and survives
// transient SMTP-server restarts without code changes.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" || cfg.Port == 0 {
		return nil, fmt.Errorf("mailer: SMTP host + port required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("mailer: SMTP from address required")
	}
	return &SMTPSender{cfg: cfg}, nil
}

func (s *SMTPSender) Send(m Message) error {
	from := m.From
	if from == "" {
		from = s.cfg.From
	}
	if len(m.To) == 0 {
		return fmt.Errorf("mailer: no recipients")
	}
	body := buildMIME(m, from)
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	if s.cfg.UseTLS {
		// Implicit TLS path — net/smtp doesn't expose a one-liner so we
		// dial manually.
		return sendImplicitTLS(addr, s.cfg.Host, auth, from, append(m.To, append(m.Cc, m.Bcc...)...), body)
	}
	return smtp.SendMail(addr, auth, from, append(m.To, append(m.Cc, m.Bcc...)...), body)
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, body []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Quit()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	return w.Close()
}

// buildMIME assembles a multipart/alternative (text + html) message,
// or single-part text/html when only one body is set.
func buildMIME(m Message, from string) []byte {
	var b bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", from)
	hdr("To", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		hdr("Cc", strings.Join(m.Cc, ", "))
	}
	hdr("Subject", m.Subject)
	hdr("MIME-Version", "1.0")
	hdr("Date", time.Now().Format(time.RFC1123Z))
	for k, v := range m.Headers {
		hdr(k, v)
	}

	switch {
	case m.HTMLBody != "" && m.TextBody != "":
		boundary := "wave-" + fmt.Sprintf("%x", time.Now().UnixNano())
		hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		fmt.Fprint(&b, "\r\n")
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", boundary, m.TextBody)
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", boundary, m.HTMLBody)
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	case m.HTMLBody != "":
		hdr("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(&b, "\r\n", m.HTMLBody)
	default:
		hdr("Content-Type", "text/plain; charset=UTF-8")
		fmt.Fprint(&b, "\r\n", m.TextBody)
	}
	return b.Bytes()
}

// ── ConsoleSender (dev) ─────────────────────────────────────────────

// ConsoleSender writes a human-readable representation of every send
// to the configured Writer (typically os.Stderr).
type ConsoleSender struct {
	w  io.Writer
	mu sync.Mutex
}

func NewConsoleSender(w io.Writer) *ConsoleSender { return &ConsoleSender{w: w} }

func (c *ConsoleSender) Send(m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.w, "─── mailer ─────────────────────────────────────────\n")
	fmt.Fprintf(c.w, "  From:    %s\n  To:      %s\n  Subject: %s\n",
		m.From, strings.Join(m.To, ", "), m.Subject)
	if m.TextBody != "" {
		fmt.Fprintf(c.w, "  ─ text ─\n%s\n", indent(m.TextBody, "    "))
	}
	if m.HTMLBody != "" {
		fmt.Fprintf(c.w, "  ─ html ─\n%s\n", indent(m.HTMLBody, "    "))
	}
	fmt.Fprintf(c.w, "─────────────────────────────────────────────────────\n")
	return nil
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// ── CaptureSender (tests) ───────────────────────────────────────────

// CaptureSender records each Send into an in-memory slice.
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

// LastTo returns the most recent message addressed to `to`, or nil.
func (c *CaptureSender) LastTo(to string) *Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.Messages) - 1; i >= 0; i-- {
		for _, r := range c.Messages[i].To {
			if r == to {
				return &c.Messages[i]
			}
		}
	}
	return nil
}

// ── Templates ───────────────────────────────────────────────────────

// RenderText renders a plain-text template.
func RenderText(tpl string, data any) (string, error) {
	t, err := texttemplate.New("m").Parse(tpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// RenderHTML renders an HTML template (auto-escaped).
func RenderHTML(tpl string, data any) (string, error) {
	t, err := template.New("m").Parse(tpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}
