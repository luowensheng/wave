// Package totp_routes implements three route types:
//
//	type: totp-enroll-start    POST /totp/enroll  → returns secret + otpauth URL
//	type: totp-enroll-confirm  POST /totp/confirm → user submits 6-digit code; persists
//	type: totp-verify          POST /totp/verify  → standalone 2FA-during-login check
//
// State for "is TOTP enrolled for user X" lives in the storage backend
// the user already has — we expose Setter / Getter / Verifier function
// hooks the orchestrator wires to its concrete impl. This keeps this
// package free of database concerns and easy to test.
package totp_routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/luowensheng/wave/infra/totp"
)

// Hook functions wired by the orchestrator.
var (
	// PendingPut stashes a freshly-issued secret keyed by user-id while
	// the user opens their authenticator app and enters the first code.
	// Pending secrets should be short-lived (5 min).
	PendingPut = func(ctx context.Context, userID, secret string) error {
		return fmt.Errorf("totp: PendingPut not wired")
	}
	// PendingGet retrieves a pending secret for the user.
	PendingGet = func(ctx context.Context, userID string) (string, error) {
		return "", fmt.Errorf("totp: PendingGet not wired")
	}
	// SecretSet persists the *confirmed* secret.
	SecretSet = func(ctx context.Context, userID, secret string) error {
		return fmt.Errorf("totp: SecretSet not wired")
	}
	// SecretGet returns the confirmed secret (or empty if not enrolled).
	SecretGet = func(ctx context.Context, userID string) (string, error) {
		return "", fmt.Errorf("totp: SecretGet not wired")
	}
	// CurrentUserID is how the route extracts "who is asking" from the
	// authenticated request context. Default returns "" (forces 401).
	CurrentUserID = func(r *http.Request) string { return "" }

	// nowFn is overridable in tests.
	nowFn = time.Now
)

// EnrollStartConfig is `type: totp-enroll-start`.
type EnrollStartConfig struct {
	Issuer string `yaml:"issuer,omitempty" json:"issuer,omitempty"` // shown in authenticator app
}

// EnrollConfirmConfig is `type: totp-enroll-confirm`.
type EnrollConfirmConfig struct{}

// VerifyConfig is `type: totp-verify`.
type VerifyConfig struct{}

// ── start ────────────────────────────────────────────────────────────────

func (c *EnrollStartConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	issuerName := c.Issuer
	if issuerName == "" {
		issuerName = "wave"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		uid := CurrentUserID(r)
		if uid == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		secret, err := totp.NewSecret(20)
		if err != nil {
			http.Error(w, "secret gen failed", http.StatusInternalServerError)
			return
		}
		if err := PendingPut(r.Context(), uid, secret); err != nil {
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"secret":      secret,
			"otpauth_url": totp.OTPAuthURL(issuerName, uid, secret),
		})
	}, nil
}

// ── confirm ──────────────────────────────────────────────────────────────

func (c *EnrollConfirmConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := CurrentUserID(r)
		if uid == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}
		secret, err := PendingGet(r.Context(), uid)
		if err != nil || secret == "" {
			http.Error(w, "no pending enrollment", http.StatusBadRequest)
			return
		}
		if !totp.Verify(secret, body.Code, nowFn()) {
			http.Error(w, "invalid code", http.StatusUnauthorized)
			return
		}
		if err := SecretSet(r.Context(), uid, secret); err != nil {
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"enrolled": true})
	}, nil
}

// ── verify (standalone 2FA check) ────────────────────────────────────────

func (c *VerifyConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := CurrentUserID(r)
		if uid == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}
		secret, err := SecretGet(r.Context(), uid)
		if err != nil || secret == "" {
			http.Error(w, "TOTP not enrolled", http.StatusForbidden)
			return
		}
		if !totp.Verify(secret, body.Code, nowFn()) {
			http.Error(w, "invalid code", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"verified": true})
	}, nil
}
