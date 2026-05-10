package features

import (
	"wave/domain"
	"net/http"
	"time"
)

// Authentication is the capability of authenticating users, issuing tokens,
// validating sessions, and signing users in/out. The struct holds the shape
// of what's possible. The orchestrator constructs concrete closures that
// wrap infra (JWT signer, user store, session store) at startup.
type Authentication struct {
	ValidateRequest func(r *http.Request, configNames ...string) (*domain.User, error)
	GenerateToken   func(user *domain.User, sessionID string, expiry time.Duration) (string, error)
	CreateSession   func(userID string, duration time.Duration) (string, error)
}
