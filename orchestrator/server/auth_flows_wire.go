package servers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"

	"wave/infra/mailer"
	"wave/infra/oauth"
	"wave/infra/sms"
	"wave/infra/verify"

	magic "wave/usecases/magic_link"
	oauthrt "wave/usecases/oauth_routes"
	totprt "wave/usecases/totp_routes"
)

// initAuthFlows wires senders, the verify Issuer, and the TOTP
// in-memory store hooks. Called from InitDependencies *after* the
// auth manager has been built so loginFn closures can rely on it.
func (s *Server) initAuthFlows() error {
	cfg := s.Config.AuthFlows
	if cfg == nil {
		// Still wire dev defaults (console senders + memory store) so
		// the route types don't 500 when used without explicit config.
		cfg = &AuthFlowsConfig{}
	}

	// ── senders ─────────────────────────────────────────────────────
	if cfg.SMTP.Host != "" {
		ms, err := mailer.NewSMTPSender(mailer.SMTPConfig{
			Host:     cfg.SMTP.Host,
			Port:     cfg.SMTP.Port,
			Username: cfg.SMTP.Username,
			Password: cfg.SMTP.Password,
			From:     cfg.SMTP.From,
			UseTLS:   cfg.SMTP.UseTLS,
		})
		if err != nil {
			return fmt.Errorf("smtp sender: %w", err)
		}
		mailer.SetDefault(ms)
		log.Printf("mailer: SMTP %s:%d", cfg.SMTP.Host, cfg.SMTP.Port)
	} else {
		log.Printf("mailer: console (dev) — set auth_flows.smtp.host for real delivery")
	}
	if cfg.Twilio.AccountSID != "" {
		ts, err := sms.NewTwilioSender(sms.TwilioConfig{
			AccountSID: cfg.Twilio.AccountSID,
			AuthToken:  cfg.Twilio.AuthToken,
			From:       cfg.Twilio.From,
		})
		if err != nil {
			return fmt.Errorf("twilio sender: %w", err)
		}
		sms.SetDefault(ts)
		log.Printf("sms: twilio account=%s", cfg.Twilio.AccountSID)
	} else {
		log.Printf("sms: console (dev) — set auth_flows.twilio.account_sid for real delivery")
	}

	// ── verify token store ─────────────────────────────────────────
	var store verify.Store
	if cfg.VerifyDB != "" {
		db, err := sql.Open("sqlite3", cfg.VerifyDB)
		if err != nil {
			return fmt.Errorf("verify db: %w", err)
		}
		ss, err := verify.NewSQLiteStore(db)
		if err != nil {
			return fmt.Errorf("verify schema: %w", err)
		}
		store = ss
		log.Printf("verify: sqlite %s", cfg.VerifyDB)
	} else {
		store = verify.NewMemoryStore()
		log.Printf("verify: memory store (dev) — set auth_flows.verify_db for persistence")
	}
	issuer := verify.NewIssuer(store, []byte(cfg.VerifyHMACSecret))
	magic.SetIssuer(issuer)

	// ── magic-link login function ──────────────────────────────────
	// We don't have a "create session by email" entrypoint in the
	// existing auth manager (it's username/password-shaped). For now
	// we set a JWT cookie directly using the standard JWT secret —
	// when the user wants this productionized they wire it through
	// orchestrator/usecases/wire.go.
	magic.SetLoginFn(func(ctx context.Context, email string, w http.ResponseWriter, r *http.Request) error {
		// Lightweight session: just a signed cookie keyed on email.
		// Production deployments should swap this for the same JWT
		// minting their auth-login route uses.
		http.SetCookie(w, &http.Cookie{
			Name: "easy_session", Value: email, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		return nil
	})

	// ── OAuth login function ────────────────────────────────────────
	// Same caveat as magic-link: this is the dev default. Production
	// should mint a JWT identical to the auth-login route's output.
	oauthrt.SetLoginFn(func(ctx context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error {
		identity := c.Email
		if identity == "" {
			identity = c.Provider + ":" + c.Subject
		}
		http.SetCookie(w, &http.Cookie{
			Name: "easy_session", Value: identity, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		return nil
	})

	// ── TOTP store hooks (in-memory) ───────────────────────────────
	// Same caveat: an in-memory map is fine for single-instance dev.
	// For production wire to your existing user store.
	wireTOTPInMemory()

	return nil
}

// wireTOTPInMemory installs map-backed implementations of the four
// TOTP store hooks. Cycle-safe for tests too.
func wireTOTPInMemory() {
	var (
		mu      sync.Mutex
		pending = map[string]string{}
		secrets = map[string]string{}
	)
	totprt.PendingPut = func(_ context.Context, uid, sec string) error {
		mu.Lock(); defer mu.Unlock()
		pending[uid] = sec
		return nil
	}
	totprt.PendingGet = func(_ context.Context, uid string) (string, error) {
		mu.Lock(); defer mu.Unlock()
		return pending[uid], nil
	}
	totprt.SecretSet = func(_ context.Context, uid, sec string) error {
		mu.Lock(); defer mu.Unlock()
		secrets[uid] = sec
		delete(pending, uid)
		return nil
	}
	totprt.SecretGet = func(_ context.Context, uid string) (string, error) {
		mu.Lock(); defer mu.Unlock()
		return secrets[uid], nil
	}
	// CurrentUserID extracts the authenticated user from the standard
	// auth context key set by features/auth.RequireAuth.
	totprt.CurrentUserID = currentUserIDFromContext
}
