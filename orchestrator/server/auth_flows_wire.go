package servers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	authfeature "github.com/luowensheng/wave/orchestrator/features/auth"

	"github.com/luowensheng/wave/infra/cookies"
	infrajwt "github.com/luowensheng/wave/infra/jwt"
	"github.com/luowensheng/wave/infra/mailer"
	"github.com/luowensheng/wave/infra/oauth"
	"github.com/luowensheng/wave/infra/sms"
	"github.com/luowensheng/wave/infra/verify"

	magic "github.com/luowensheng/wave/usecases/magic_link"
	oauthrt "github.com/luowensheng/wave/usecases/oauth_routes"
	totprt "github.com/luowensheng/wave/usecases/totp_routes"
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

	// ── magic-link + OAuth login functions ──────────────────────────
	// Mint a real HS256 JWT signed with the same SECRET_KEY the rest
	// of the auth subsystem uses, and set it on the cookie name
	// configured in `auth.session.cookie_name` (or whichever JWT auth
	// config exists). Falls back to "wave_session" when no auth: block
	// is declared.
	//
	// Users who need richer session creation (database-backed sessions,
	// custom claims, etc.) override these via SetLoginFn from their own
	// wiring code — see orchestrator/usecases/wire.go for the seam.
	sessionCfg := pickSessionAuthConfig(s.Config.Auth)
	magic.SetLoginFn(func(ctx context.Context, email string, w http.ResponseWriter, r *http.Request) error {
		return mintSessionJWT(email, sessionCfg, w, r)
	})
	oauthrt.SetLoginFn(func(ctx context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error {
		identity := c.Email
		if identity == "" {
			identity = c.Provider + ":" + c.Subject
		}
		return mintSessionJWT(identity, sessionCfg, w, r)
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

// pickSessionAuthConfig returns the AuthConfig the dev login functions
// should use when minting their JWT cookie. Picks the first JWT-typed
// config (the most common shape). When no auth: block is declared at
// all, returns nil and the helpers fall back to safe defaults.
func pickSessionAuthConfig(configs map[string]*authfeature.AuthConfig) *authfeature.AuthConfig {
	if len(configs) == 0 {
		return nil
	}
	// Prefer one explicitly named "session" (matches the demo apps),
	// otherwise the first JWT-typed config we find.
	if c, ok := configs["session"]; ok {
		return c
	}
	for _, c := range configs {
		if c == nil {
			continue
		}
		if c.Type == "" || c.Type == "default" || c.Type == "jwt" {
			return c
		}
	}
	for _, c := range configs {
		if c != nil {
			return c
		}
	}
	return nil
}

// mintSessionJWT signs an HS256 JWT for the given identity (email or
// provider:subject) and writes it as a cookie using the policy from
// the supplied AuthConfig. The signing secret comes from $SECRET_KEY
// (matching the rest of the auth subsystem). Used by the magic-link
// and OAuth dev login paths.
func mintSessionJWT(identity string, cfg *authfeature.AuthConfig, w http.ResponseWriter, r *http.Request) error {
	secret := jwtSecret(cfg)
	if len(secret) == 0 {
		return fmt.Errorf("SECRET_KEY is unset; cannot mint session JWT")
	}

	// jwt.Sign requires UserID > 0 + non-empty username + non-empty
	// session ID. We don't have a real user store here (magic-link
	// signs people in by email alone), so derive deterministic stand-ins
	// from the identity string. Production should swap this for a real
	// user lookup via SetLoginFn.
	uid := stableUserID(identity)
	sessionID := newSessionID()
	expiry := tokenExpiry(cfg)
	tok, err := infrajwt.Sign(secret, uid, identity, sessionID, expiry)
	if err != nil {
		return fmt.Errorf("sign jwt: %w", err)
	}

	cookieName, secure, sameSite, domain := cookieParamsFromAuthConfig(cfg)
	maxAge := int(expiry.Seconds())
	c := cookies.Build(cookieName, tok, cookies.Policy{
		Secure:      secure,
		SameSiteRaw: sameSite,
		Domain:      domain,
	}, r, maxAge)
	http.SetCookie(w, c)
	return nil
}

func jwtSecret(cfg *authfeature.AuthConfig) []byte {
	if cfg != nil && cfg.Secret != "" {
		return []byte(cfg.Secret)
	}
	if v := os.Getenv("SECRET_KEY"); v != "" {
		return []byte(v)
	}
	return nil
}

func cookieParamsFromAuthConfig(cfg *authfeature.AuthConfig) (name string, secure *bool, sameSite, domain string) {
	name = "wave_session"
	if cfg != nil {
		if cfg.CookieName != "" {
			name = cfg.CookieName
		}
		secure = cfg.SecureCookie
		sameSite = cfg.CookieSameSite
		domain = cfg.CookieDomain
	}
	return
}

func tokenExpiry(cfg *authfeature.AuthConfig) time.Duration {
	if cfg != nil && cfg.TokenDurationSeconds > 0 {
		return time.Duration(cfg.TokenDurationSeconds) * time.Second
	}
	return 24 * time.Hour
}

// stableUserID derives a positive int from a string identity, so the
// jwt.Sign UserID > 0 invariant holds. Stable per identity.
func stableUserID(identity string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(identity))
	v := int(h.Sum32() & 0x7fffffff)
	if v == 0 {
		v = 1
	}
	return v
}

// newSessionID returns 16 random hex chars suitable for use as a JWT
// session-id claim. crypto/rand failures fall back to a timestamp so
// we never panic in the login path.
func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
