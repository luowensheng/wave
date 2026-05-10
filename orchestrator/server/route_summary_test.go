package servers

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRouteSummariesFromConfig(t *testing.T) {
	// Build a Config with already-materialized routes — this exercises
	// the happy path RouteSummaries takes when called after Start.
	yamlText := `routes:
- path: /a
  method: GET
  type: static
  static: { dir: ./pub }
- path: /b
  method: POST
  type: api
  description: do a thing
  auth: ["jwt"]
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatal(err)
	}
	// loadConfig leaves Routes empty until renderVars; force-materialize
	// from the raw block here.
	if b, err := cfg.RawRoutes.Bytes(); err == nil && len(b) > 0 {
		if err := yaml.Unmarshal(b, &cfg.Routes); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Config: &cfg}
	rows, err := s.RouteSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Path != "/a" || rows[0].Type != "static" {
		t.Errorf("rows[0] = %+v", rows[0])
	}
	if rows[1].Description != "do a thing" || len(rows[1].Auth) != 1 || rows[1].Auth[0] != "jwt" {
		t.Errorf("rows[1] = %+v", rows[1])
	}
}

func TestRouteSummariesEmpty(t *testing.T) {
	s := &Server{Config: &Config{}}
	rows, err := s.RouteSummaries()
	if err != nil || len(rows) != 0 {
		t.Errorf("got %d rows, err=%v", len(rows), err)
	}
}
