// echo is a minimal subprocess plugin that demonstrates the JSON-in /
// JSON-out contract used by `transport: process` plugins.
//
// Build:   go build -o echo ./examples/plugins/echo
// Wire it: see examples/plugins/echo/server.yaml
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	TriggerKey string            `json:"trigger_key"`
	Metadata   map[string]string `json:"metadata"`
	Headers    map[string]string `json:"headers"`
	Cookies    map[string]string `json:"cookies"`
	Query      map[string]string `json:"query"`
	Body       json.RawMessage   `json:"body"`
}

type response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}

	body, _ := json.Marshal(map[string]any{
		"echoed":      json.RawMessage(req.Body),
		"trigger_key": req.TriggerKey,
		"remote_ip":   req.Metadata["remote_ip"],
	})

	resp := response{
		Status:  200,
		Headers: map[string]string{"X-Echo-Plugin": "1"},
		Body:    body,
	}
	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
