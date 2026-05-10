package webhooksig

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Round-trip every provider: SignRequest → Verify on the same bytes
// must succeed.
func TestSignVerifyRoundTrip(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	now := time.Now()
	cases := []struct {
		name string
		sign SignConfig
		ver  Config
	}{
		{
			"stripe",
			SignConfig{Provider: ProviderStripe, Secret: "k", NowFn: func() time.Time { return now }},
			Config{Provider: ProviderStripe, Secret: "k", NowFn: func() time.Time { return now }},
		},
		{
			"github",
			SignConfig{Provider: ProviderGitHub, Secret: "g"},
			Config{Provider: ProviderGitHub, Secret: "g"},
		},
		{
			"slack",
			SignConfig{Provider: ProviderSlack, Secret: "s", NowFn: func() time.Time { return now }},
			Config{Provider: ProviderSlack, Secret: "s", NowFn: func() time.Time { return now }},
		},
		{
			"generic",
			SignConfig{Provider: ProviderGeneric, Secret: "x", Header: "X-Sig", HeaderPrefix: "sha256="},
			Config{Provider: ProviderGeneric, Secret: "x", Header: "X-Sig", HeaderPrefix: "sha256="},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
			if err := SignRequest(r, body, c.sign); err != nil {
				t.Fatal(err)
			}
			// Reset body since SignRequest didn't consume it but Verify will.
			r.Body = http.NoBody
			r.Body = httptest.NewRequest("POST", "/", bytes.NewReader(body)).Body
			v, err := New(c.ver)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Verify(r); err != nil {
				t.Errorf("verify after sign failed: %v", err)
			}
		})
	}
}

func TestSignRejectsEmptySecret(t *testing.T) {
	r := httptest.NewRequest("POST", "/", bytes.NewReader(nil))
	if err := SignRequest(r, nil, SignConfig{Provider: ProviderStripe}); err == nil {
		t.Error("expected empty-secret error")
	}
}

func TestSignUnknownProvider(t *testing.T) {
	r := httptest.NewRequest("POST", "/", bytes.NewReader(nil))
	if err := SignRequest(r, nil, SignConfig{Provider: "nope", Secret: "k"}); err == nil {
		t.Error("expected unknown-provider error")
	}
}
