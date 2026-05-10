// Package jwt is a thin adapter around github.com/golang-jwt/jwt/v5 that
// signs and parses HS256 tokens carrying our session/user claims. It has
// no awareness of the wave auth feature it ultimately serves.
package jwt

import (
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// Claims is the payload carried by every wave session token.
type Claims struct {
	UserID    int                `json:"user_id"`
	Username  string             `json:"username"`
	Time      *gojwt.NumericDate `json:"time"`
	SessionID string             `json:"session_id"`
	gojwt.RegisteredClaims
}

// serverStartupTime is set on package init and used to invalidate
// tokens that were issued before this server process started.
var serverStartupTime = time.Now()

func (c *Claims) Valid() error {
	now := time.Now()

	if c.Time == nil || c.Time.Time.Before(serverStartupTime) {
		return errors.New("token issued before server startup")
	}

	if c.IssuedAt != nil && c.IssuedAt.Time.Before(serverStartupTime) {
		return errors.New("token issued before server startup")
	}

	if c.ExpiresAt != nil && now.After(c.ExpiresAt.Time) {
		return fmt.Errorf("token has expired: %s", c.ExpiresAt.Time.Format(time.DateTime))
	}

	if c.NotBefore != nil && now.Before(c.NotBefore.Time) {
		return errors.New("token used before valid")
	}

	if c.UserID <= 0 {
		return errors.New("invalid user ID")
	}

	if c.Username == "" {
		return errors.New("username is required")
	}

	if c.SessionID == "" {
		return errors.New("session ID is required")
	}

	return nil
}

// Sign builds and HS256-signs a token from the given claim fields.
func Sign(secret []byte, userID int, username, sessionID string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    userID,
		Username:  username,
		SessionID: sessionID,
		Time:      gojwt.NewNumericDate(now),
		RegisteredClaims: gojwt.RegisteredClaims{
			ExpiresAt: gojwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  gojwt.NewNumericDate(now),
			NotBefore: gojwt.NewNumericDate(now),
		},
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// Parse verifies a token signature and returns its claims.
func Parse(secret []byte, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := gojwt.ParseWithClaims(tokenString, claims, func(token *gojwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
