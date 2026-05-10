package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B reference vectors use a 20-byte ASCII key
// "12345678901234567890" (=> base32 "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ").
// We pin a few test values so we know our implementation matches the
// canonical algorithm.
const refSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestRFC6238Vectors(t *testing.T) {
	cases := []struct {
		ts   int64
		want string
	}{
		// 6-digit truncations of RFC vectors.
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
	}
	for _, c := range cases {
		got, err := Generate(refSecret, time.Unix(c.ts, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("Generate(%d) = %q, want %q", c.ts, got, c.want)
		}
	}
}

func TestGenerateVerifyRoundTrip(t *testing.T) {
	secret, err := NewSecret(20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := Generate(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(secret, code, now) {
		t.Errorf("verify failed for fresh code")
	}
}

func TestVerifySkewTolerance(t *testing.T) {
	secret, _ := NewSecret(20)
	now := time.Now()
	prev, _ := Generate(secret, now.Add(-30*time.Second))
	if !Verify(secret, prev, now) {
		t.Error("previous-step code should verify within skew")
	}
	farPast, _ := Generate(secret, now.Add(-3*time.Minute))
	if Verify(secret, farPast, now) {
		t.Error("far-past code should not verify")
	}
}

func TestVerifyBadCode(t *testing.T) {
	secret, _ := NewSecret(20)
	if Verify(secret, "abcdef", time.Now()) {
		t.Error("non-numeric should fail")
	}
	if Verify(secret, "12345", time.Now()) {
		t.Error("wrong-length should fail")
	}
}

func TestOTPAuthURLContainsParams(t *testing.T) {
	u := OTPAuthURL("MyApp", "alice@x.io", refSecret)
	for _, want := range []string{"otpauth://totp/", "MyApp", "alice", refSecret, "digits=6", "period=30", "issuer=MyApp"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
}

func TestNewSecretEntropy(t *testing.T) {
	a, _ := NewSecret(20)
	b, _ := NewSecret(20)
	if a == b {
		t.Error("two consecutive secrets should differ")
	}
	if len(a) < 16 {
		t.Errorf("secret too short: %q", a)
	}
}
