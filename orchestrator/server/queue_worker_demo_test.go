package servers

import (
	"path/filepath"
	"testing"
)

// TestQueueWorkerDemo_Parses verifies that the queue-worker-demo
// server.yaml loads cleanly into the Config struct. This catches
// breakage when sink/action shapes change — the demo file is a
// canary that the new for_each + api sink syntax stays valid.
func TestQueueWorkerDemo_Parses(t *testing.T) {
	demoPath := filepath.Join("..", "..", "examples", "apps", "queue-worker-demo", "server.yaml")
	// Load through loadConfig so the two-phase resolver populates the
	// typed resource maps (requests/etc.) the way `wave serve` does.
	cfgPtr, err := loadConfig(demoPath)
	if err != nil {
		t.Fatalf("load demo: %v", err)
	}
	cfg := *cfgPtr

	// requests: block — one named def
	if len(cfg.Requests) != 1 {
		t.Fatalf("expected 1 request def, got %d", len(cfg.Requests))
	}
	fetchURL, ok := cfg.Requests["fetch_url"]
	if !ok {
		t.Fatal("missing requests[fetch_url]")
	}
	if fetchURL.URL != "{{url}}" {
		t.Errorf("fetch_url.URL = %q, want %q", fetchURL.URL, "{{url}}")
	}
	if fetchURL.Method != "GET" {
		t.Errorf("fetch_url.Method = %q, want GET", fetchURL.Method)
	}

	// schedule: block — one job using for_each + api
	if len(cfg.Schedule) != 1 {
		t.Fatalf("expected 1 scheduled job, got %d", len(cfg.Schedule))
	}
	job, ok := cfg.Schedule["drain_queue"]
	if !ok {
		t.Fatal("missing schedule[drain_queue]")
	}
	if job.Every != "5s" {
		t.Errorf("job.Every = %q, want 5s", job.Every)
	}
	if job.Action == nil {
		t.Fatal("job.Action is nil")
	}
	if job.Action.Output != "peek" {
		t.Errorf("action.Output = %q, want peek", job.Action.Output)
	}

	// then: should contain a single for_each sink
	if len(job.Then) != 1 {
		t.Fatalf("expected 1 then sink, got %d", len(job.Then))
	}
	forEach := job.Then[0]
	if forEach.Type != "for_each" {
		t.Errorf("sink.Type = %q, want for_each", forEach.Type)
	}
	if forEach.In != "peek.data" {
		t.Errorf("for_each.In = %q, want peek.data", forEach.In)
	}
	if forEach.As != "task" {
		t.Errorf("for_each.As = %q, want task", forEach.As)
	}
	if len(forEach.Do) != 3 {
		t.Fatalf("expected 3 nested do sinks, got %d", len(forEach.Do))
	}

	// First nested sink: api with ref
	apiSink := forEach.Do[0]
	if apiSink.Type != "api" {
		t.Errorf("do[0].Type = %q, want api", apiSink.Type)
	}
	if apiSink.Ref != "fetch_url" {
		t.Errorf("do[0].Ref = %q, want fetch_url", apiSink.Ref)
	}
	if apiSink.Output != "resp" {
		t.Errorf("do[0].Output = %q, want resp", apiSink.Output)
	}
	if got := apiSink.Vars["url"]; got != "task.url" {
		t.Errorf("do[0].Vars[url] = %q, want task.url", got)
	}

	// Second + third nested sinks: storage with explicit inputs
	insertSink := forEach.Do[1]
	if insertSink.Type != "storage" || insertSink.Source != "q" {
		t.Errorf("do[1] not storage/q: %+v", insertSink)
	}
	if got := insertSink.Inputs["body"]; got != "resp.text" {
		t.Errorf("do[1].Inputs[body] = %q, want resp.text (string — proves api sink output is reachable)", got)
	}

	updateSink := forEach.Do[2]
	if got := updateSink.Inputs["id"]; got != "task.id" {
		t.Errorf("do[2].Inputs[id] = %q, want task.id (proves iteration var is reachable)", got)
	}

	// Defaults block — confirms expected_content_type override works at root
	if cfg.Defaults == nil {
		t.Fatal("Defaults is nil")
	}
	if cfg.Defaults.ExpectedContentType != "application/json" {
		t.Errorf("default.expected_content_type = %q, want application/json",
			cfg.Defaults.ExpectedContentType)
	}
}
