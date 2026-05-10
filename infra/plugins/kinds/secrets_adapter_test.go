package kinds

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSecretsAdapterResolve(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`{"value":"c2VjcmV0"}`)} // base64("secret")
	a := &secretsAdapter{rpc: rc}
	v, err := a.Resolve(context.Background(), "vault://kv/foo")
	if err != nil {
		t.Fatal(err)
	}
	if string(v) != "secret" {
		t.Errorf("got %q", v)
	}
	if rc.last.method != MethodSecretsResolve {
		t.Errorf("method = %s", rc.last.method)
	}
}
