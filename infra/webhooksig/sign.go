package webhooksig

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"time"
)

// SignRequest stamps an outgoing request with an HMAC header so the
// receiving end can verify it via the matching Verifier in this same
// package. Mirrors the wire formats we accept inbound.
//
// Usage:
//
//	body := []byte(...)
//	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
//	webhooksig.SignRequest(req, body, webhooksig.SignConfig{
//	  Provider: webhooksig.ProviderStripe,
//	  Secret:   secret,
//	})
//
// For the generic provider, sets `<header>: <prefix><hex>`. For Stripe
// it sets `Stripe-Signature: t=<unix>,v1=<hex>`. For GitHub it sets
// `X-Hub-Signature-256: sha256=<hex>`. Slack sets both
// `X-Slack-Request-Timestamp` and `X-Slack-Signature`.
type SignConfig struct {
	Provider     string
	Secret       string
	Header       string // generic only
	Algorithm    string // generic only ("sha256" default | "sha1")
	HeaderPrefix string // generic only
	NowFn        func() time.Time
}

// SignRequest signs r in place. body is the request body that will be
// sent on the wire (signing must use the same bytes the verifier
// reads).
func SignRequest(r *http.Request, body []byte, cfg SignConfig) error {
	if cfg.Secret == "" {
		return fmt.Errorf("webhooksig: empty secret")
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	switch cfg.Provider {
	case ProviderStripe:
		ts := strconv.FormatInt(cfg.NowFn().Unix(), 10)
		m := hmac.New(sha256.New, []byte(cfg.Secret))
		m.Write([]byte(ts))
		m.Write([]byte("."))
		m.Write(body)
		r.Header.Set("Stripe-Signature", "t="+ts+",v1="+hex.EncodeToString(m.Sum(nil)))
	case ProviderGitHub:
		m := hmac.New(sha256.New, []byte(cfg.Secret))
		m.Write(body)
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(m.Sum(nil)))
	case ProviderSlack:
		ts := strconv.FormatInt(cfg.NowFn().Unix(), 10)
		m := hmac.New(sha256.New, []byte(cfg.Secret))
		fmt.Fprintf(m, "v0:%s:", ts)
		m.Write(body)
		r.Header.Set("X-Slack-Request-Timestamp", ts)
		r.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(m.Sum(nil)))
	case ProviderGeneric, "":
		header := cfg.Header
		if header == "" {
			header = "X-Signature"
		}
		algo := cfg.Algorithm
		if algo == "" {
			algo = "sha256"
		}
		m, err := hmacFor(algo, cfg.Secret)
		if err != nil {
			return err
		}
		m.Write(body)
		r.Header.Set(header, cfg.HeaderPrefix+hex.EncodeToString(m.Sum(nil)))
	default:
		return fmt.Errorf("webhooksig: unknown provider %q", cfg.Provider)
	}
	return nil
}

func hmacFor(algo, secret string) (hash.Hash, error) {
	switch algo {
	case "sha256":
		return hmac.New(sha256.New, []byte(secret)), nil
	case "sha1":
		return hmac.New(sha1.New, []byte(secret)), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algo)
	}
}
