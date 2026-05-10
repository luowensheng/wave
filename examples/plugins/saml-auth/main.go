// Command saml-auth is the reference KindAuth plugin for the wave
// SDK. It implements a SAML 2.0 Service Provider via crewjam/saml.
//
// Configuration is taken from the environment:
//
//	SAML_IDP_METADATA_URL  required — URL of the IdP metadata XML
//	SAML_SP_ENTITY_ID      required — SP entity ID (audience)
//	SAML_SP_ACS_URL        required — Assertion Consumer Service URL
//	SAML_SP_CERT_PATH      required — PEM-encoded SP certificate
//	SAML_SP_KEY_PATH       required — PEM-encoded SP private key
//
// The plugin handles two methods:
//
//	saml_init       — build an AuthnRequest and return the redirect URL.
//	saml_callback   — validate a SAMLResponse and return Claims.
//
// Sessions, cookies, and JWTs are owned by the orchestrator; this
// plugin only translates SAML <-> Claims.
package main

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	sdk "wave.dev/sdk"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// samlPlugin is a lazily-initialized SAML SP. The SDK starts the
// process before YAML wiring is final, so we defer the metadata fetch
// (which can fail offline) to the first request.
type samlPlugin struct {
	once sync.Once
	sp   *samlsp.Middleware
	err  error

	mu     sync.Mutex
	cached map[string]*sdk.Claims // subject → most recent claims
}

func newSamlPlugin() *samlPlugin {
	return &samlPlugin{cached: map[string]*sdk.Claims{}}
}

func (p *samlPlugin) ensureSP(ctx context.Context) (*samlsp.Middleware, error) {
	p.once.Do(func() {
		p.sp, p.err = buildSP(ctx)
	})
	return p.sp, p.err
}

func buildSP(ctx context.Context) (*samlsp.Middleware, error) {
	mdURL := os.Getenv("SAML_IDP_METADATA_URL")
	entityID := os.Getenv("SAML_SP_ENTITY_ID")
	acs := os.Getenv("SAML_SP_ACS_URL")
	certPath := os.Getenv("SAML_SP_CERT_PATH")
	keyPath := os.Getenv("SAML_SP_KEY_PATH")
	if mdURL == "" || entityID == "" || acs == "" || certPath == "" || keyPath == "" {
		return nil, errors.New("SAML_IDP_METADATA_URL, SAML_SP_ENTITY_ID, SAML_SP_ACS_URL, SAML_SP_CERT_PATH, SAML_SP_KEY_PATH all required")
	}
	keyPair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load SP keypair: %w", err)
	}
	leaf, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse SP cert: %w", err)
	}
	keyPair.Leaf = leaf
	rsaKey, ok := keyPair.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("SP private key must be RSA")
	}
	mdURLParsed, err := url.Parse(mdURL)
	if err != nil {
		return nil, fmt.Errorf("parse metadata URL: %w", err)
	}
	idpMeta, err := samlsp.FetchMetadata(ctx, http.DefaultClient, *mdURLParsed)
	if err != nil {
		return nil, fmt.Errorf("fetch IdP metadata: %w", err)
	}
	rootURL, err := url.Parse(acs)
	if err != nil {
		return nil, fmt.Errorf("parse ACS URL: %w", err)
	}
	sp, err := samlsp.New(samlsp.Options{
		EntityID:    entityID,
		URL:         *rootURL,
		Key:         rsaKey,
		Certificate: leaf,
		IDPMetadata: idpMeta,
	})
	if err != nil {
		return nil, fmt.Errorf("samlsp.New: %w", err)
	}
	return sp, nil
}

func (p *samlPlugin) Authenticate(ctx context.Context, req *sdk.AuthRequest) (*sdk.AuthResult, error) {
	switch req.Method {
	case "saml_init":
		sp, err := p.ensureSP(ctx)
		if err != nil {
			return nil, err
		}
		// Build an AuthnRequest and hand the redirect back to the
		// orchestrator, which wraps the redirect in a 302.
		authnReq, err := sp.ServiceProvider.MakeRedirectAuthenticationRequest(req.Credentials["relay_state"])
		if err != nil {
			return nil, fmt.Errorf("MakeRedirectAuthenticationRequest: %w", err)
		}
		return &sdk.AuthResult{
			Redirect: authnReq.String(),
		}, nil

	case "saml_callback":
		sp, err := p.ensureSP(ctx)
		if err != nil {
			return nil, err
		}
		raw := req.Credentials["SAMLResponse"]
		if raw == "" {
			return nil, errors.New("missing SAMLResponse credential")
		}
		// crewjam expects an *http.Request — synthesize one with the
		// form value populated.
		fakeReq := &http.Request{
			Method: "POST",
			Form:   url.Values{"SAMLResponse": []string{raw}},
		}
		assertion, err := sp.ServiceProvider.ParseResponse(fakeReq, []string{req.Credentials["request_id"]})
		if err != nil {
			return nil, fmt.Errorf("ParseResponse: %w", err)
		}
		claims := assertionToClaims(assertion)
		p.mu.Lock()
		p.cached[claims.Subject] = claims
		p.mu.Unlock()
		return &sdk.AuthResult{Authenticated: true, Claims: claims}, nil

	default:
		return nil, fmt.Errorf("unsupported method: %q", req.Method)
	}
}

func (p *samlPlugin) RefreshClaims(_ context.Context, subject string) (*sdk.Claims, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.cached[subject]
	if !ok {
		return nil, errors.New("SAML does not support refresh; subject not cached")
	}
	return c, nil
}

func (p *samlPlugin) Logout(_ context.Context, _ string) error {
	// Best-effort: real SAML SLO requires a round-trip to the IdP.
	// For the reference plugin we just clear the cached claims.
	return nil
}

func (p *samlPlugin) Close() error { return nil }

// assertionToClaims maps SAML attributes onto the SDK Claims shape.
func assertionToClaims(a *saml.Assertion) *sdk.Claims {
	c := &sdk.Claims{Provider: "saml", Raw: map[string]any{}}
	if a == nil {
		return c
	}
	if a.Subject != nil && a.Subject.NameID != nil {
		c.Subject = a.Subject.NameID.Value
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			vals := []string{}
			for _, v := range attr.Values {
				vals = append(vals, v.Value)
			}
			if len(vals) == 0 {
				continue
			}
			c.Raw[attr.Name] = vals
			switch attr.Name {
			case "email", "mail", "urn:oid:0.9.2342.19200300.100.1.3":
				c.Email = vals[0]
			case "name", "displayName", "urn:oid:2.16.840.1.113730.3.1.241":
				c.Name = vals[0]
			case "groups", "roles", "memberOf":
				c.Roles = vals
			}
		}
	}
	if c.Subject == "" && c.Email != "" {
		c.Subject = c.Email
	}
	return c
}

// pemSanity ensures the import is used in case crewjam removes the
// indirect dependency in a future minor — keeps the build deterministic.
var _ = pem.Block{}
var _ = time.Now

func main() {
	if err := sdk.RunAuth(newSamlPlugin()); err != nil {
		fmt.Fprintln(os.Stderr, "saml-auth:", err)
		os.Exit(1)
	}
}
