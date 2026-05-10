package features

// make_authentication.go wires the concrete auth manager into the
// features.Authentication capability struct so callers can depend on
// the struct-of-funcs instead of importing the orchestrator auth package.

import (
	"net/http"
	"time"

	"wave/domain"
	capfeatures "wave/features"
	authfeature "wave/orchestrator/features/auth"
)

// MakeAuthentication returns a populated features.Authentication by closing
// over the already-initialized authManager (set by auth.InitAuthManager).
// Must be called after auth.InitAuthManager.
func MakeAuthentication() capfeatures.Authentication {
	return capfeatures.Authentication{
		ValidateRequest: func(r *http.Request, configNames ...string) (*domain.User, error) {
			_, err := authfeature.ValidateSignIn(r)
			if err != nil {
				return nil, err
			}
			// Retrieve user injected into context by RequireAuth middleware.
			if pub, ok := r.Context().Value(authfeature.UserContextKey).(*authfeature.PublicUser); ok {
				return &domain.User{ID: pub.ID, Username: pub.Username}, nil
			}
			return nil, nil
		},
		GenerateToken: func(user *domain.User, sessionID string, expiry time.Duration) (string, error) {
			return authfeature.GenerateJWT(&authfeature.User{
				ID:       user.ID,
				Username: user.Username,
			}, sessionID, expiry)
		},
		CreateSession: func(userID string, duration time.Duration) (string, error) {
			return authfeature.CreateSession(userID, duration)
		},
	}
}
