package kinds

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAuthAdapterAuthenticate(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`{"authenticated":true,"claims":{"subject":"u1"}}`)}
	a := &authAdapter{rpc: rc}
	res, err := a.Authenticate(context.Background(), &AuthRequest{Method: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Authenticated || res.Claims.Subject != "u1" {
		t.Errorf("got %+v", res)
	}
	if rc.last.method != MethodAuthAuthenticate {
		t.Errorf("method = %s", rc.last.method)
	}
}

func TestAuthAdapterRefreshClaims(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`{"subject":"s","email":"e@x"}`)}
	a := &authAdapter{rpc: rc}
	c, err := a.RefreshClaims(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if c.Email != "e@x" {
		t.Errorf("got %+v", c)
	}
}

func TestAuthAdapterLogout(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &authAdapter{rpc: rc}
	if err := a.Logout(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodAuthLogout {
		t.Errorf("method = %s", rc.last.method)
	}
}
