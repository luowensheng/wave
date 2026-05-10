package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "wave.dev/sdk"
)

// The handler is small enough that the unit test calls it directly. The
// SDK transport is exercised by the SDK module's own tests.
func TestEchoHandler(t *testing.T) {
	body := json.RawMessage(`"hello"`)
	resp, err := echo{}.Call(context.Background(), &sdk.Request{TriggerKey: "k", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("status = %d", resp.Status)
	}
	if !strings.Contains(string(resp.Body), `"trigger_key":"k"`) {
		t.Errorf("missing trigger key: %s", resp.Body)
	}
	if !strings.Contains(string(resp.Body), `"echo":"hello"`) {
		t.Errorf("missing echoed body: %s", resp.Body)
	}
}
