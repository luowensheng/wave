// Command echo-handler is the reference handler-kind plugin for the
// new wave SDK. It echoes the trigger key and request body back
// in a 200 response.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sdk "wave.dev/sdk"
)

type echo struct{}

func (echo) Call(_ context.Context, req *sdk.Request) (*sdk.Response, error) {
	body, err := json.Marshal(map[string]any{
		"trigger_key": req.TriggerKey,
		"echo":        json.RawMessage(req.Body),
	})
	if err != nil {
		return nil, err
	}
	return &sdk.Response{Status: 200, Body: body}, nil
}

func (echo) Close() error { return nil }

func main() {
	if err := sdk.RunHandler(echo{}); err != nil {
		fmt.Fprintln(os.Stderr, "echo-handler:", err)
		os.Exit(1)
	}
}
