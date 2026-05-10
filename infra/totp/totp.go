// Package totp implements RFC 6238 TOTP, compatible with Google
// Authenticator / 1Password / Authy / Aegis. Pure stdlib; no external
// dependencies.
//
// Standard parameters:
//   - HMAC-SHA1
//   - 30-second time step
//   - 6-digit codes
//   - ±1 step skew tolerance for clock drift
//
// Workflow:
//
//   secret := totp.NewSecret()                               // generate
//   url := totp.OTPAuthURL("MyApp", "alice@x.io", secret)    // QR-encodable
//   ok := totp.Verify(secret, userInput, time.Now())         // 30s window
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDigits = 6
	defaultStep   = 30 * time.Second
	defaultSkew   = 1 // accept previous + next steps
)

// NewSecret returns a base32-encoded random secret of the given byte
// length (default 20 = 160 bits, RFC 4226 recommendation).
func NewSecret(size int) (string, error) {
	if size <= 0 {
		size = 20
	}
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "="), nil
}

// Generate returns the 6-digit TOTP code for `secret` at time t.
func Generate(secret string, t time.Time) (string, error) {
	return generate(secret, t.UTC().Unix()/int64(defaultStep.Seconds()), defaultDigits)
}

// Verify returns true if `code` matches the TOTP for `secret` at time
// t (or within ±1 step). Constant-time comparison.
func Verify(secret, code string, t time.Time) bool {
	if len(code) != defaultDigits {
		return false
	}
	step := t.UTC().Unix() / int64(defaultStep.Seconds())
	for d := -defaultSkew; d <= defaultSkew; d++ {
		want, err := generate(secret, step+int64(d), defaultDigits)
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

func generate(secret string, counter int64, digits int) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(counter))
	h := hmac.New(sha1.New, key)
	h.Write(c[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 |
		uint32(sum[off+1])<<16 |
		uint32(sum[off+2])<<8 |
		uint32(sum[off+3])
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod), nil
}

func decodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	// Pad to a multiple of 8 if the user trimmed = signs.
	if pad := len(s) % 8; pad > 0 {
		s += strings.Repeat("=", 8-pad)
	}
	b, err := base32.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("invalid base32 secret")
	}
	return b, nil
}

// OTPAuthURL builds the otpauth:// URL accepted by every authenticator
// app. account is typically the user's email, issuer is the product
// name (shown above the code in the app).
func OTPAuthURL(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	label := url.PathEscape(issuer + ":" + account)
	return "otpauth://totp/" + label + "?" + v.Encode()
}
