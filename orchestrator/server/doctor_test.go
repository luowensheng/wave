package servers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"wave/infra/plugins"
	"wave/usecases/routes"
)

func TestDoctorReportsPluginIssues(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\nexit 0"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Server{Config: &Config{
		Plugins: map[string]*plugins.PluginConfig{
			"good_plugin": {Transport: "process", Command: good},
			"bad_plugin":  {Transport: "process", Command: "/no/such/binary"},
			"grpc_stub":   {Transport: "grpc", Address: "unix:///nope.sock"},
		},
	}}
	results, failures := s.RunDoctor(context.Background())
	got := map[string]CheckResult{}
	for _, r := range results {
		got[r.Name] = r
	}
	if got["plugin:good_plugin"].Status != "ok" {
		t.Errorf("good_plugin = %+v", got["plugin:good_plugin"])
	}
	if got["plugin:bad_plugin"].Status != "fail" {
		t.Errorf("bad_plugin = %+v", got["plugin:bad_plugin"])
	}
	if got["plugin:grpc_stub"].Status != "warn" {
		t.Errorf("grpc_stub should warn (stub transport), got %+v", got["plugin:grpc_stub"])
	}
	if failures < 1 {
		t.Errorf("expected at least one failure, got %d", failures)
	}
}

func TestDoctorReportsMissingFiles(t *testing.T) {
	s := &Server{Config: &Config{
		Routes: []*Route{
			{Path: "/x", Type: "file", FileConfig: &routes.FileConfig{FilePath: "/nonexistent/x.html"}},
			{Path: "/y", Type: "static", StaticDirConfig: &routes.StaticConfig{Dir: "/nonexistent/dir"}},
		},
	}}
	results, _ := s.RunDoctor(context.Background())
	got := map[string]CheckResult{}
	for _, r := range results {
		got[r.Name] = r
	}
	if got["route:/x"].Status != "warn" {
		t.Errorf("file route check missing/wrong: %+v", got["route:/x"])
	}
	if got["route:/y"].Status != "warn" {
		t.Errorf("static route check missing/wrong: %+v", got["route:/y"])
	}
}

func TestDoctorEmptyConfigOK(t *testing.T) {
	s := &Server{Config: &Config{}}
	_, failures := s.RunDoctor(context.Background())
	if failures != 0 {
		t.Errorf("empty config should have 0 failures, got %d", failures)
	}
}
