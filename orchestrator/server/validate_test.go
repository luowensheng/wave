package servers

import (
	"strings"
	"testing"

	"github.com/luowensheng/wave/infra/connections"
	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/usecases/routes"
)

func newCfg() *Config {
	return &Config{
		Plugins: map[string]*plugins.PluginConfig{
			"echo": {Transport: "process", Command: "./echo"},
		},
		Connections: map[string]*connections.ConnectionConfig{
			"payments": {Type: "sse", SubscribePath: "/events/payments"},
		},
	}
}

func TestValidateConfigOK(t *testing.T) {
	s := &Server{Config: newCfg()}
	s.Config.Routes = []*Route{
		{Path: "/echo", Method: "POST", Type: "plugin", PluginConfig: &routes.PluginConfig{Name: "echo"}},
		{Path: "/web", Method: "POST", Type: "stream-publish",
			StreamPublishConfig: &routes.StreamPublishConfig{Connection: "payments"}},
	}
	if err := s.ValidateConfig(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateConfigUnknownPlugin(t *testing.T) {
	s := &Server{Config: newCfg()}
	s.Config.Routes = []*Route{
		{Path: "/x", Method: "POST", Type: "plugin", PluginConfig: &routes.PluginConfig{Name: "missing"}},
	}
	err := s.ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), `unknown plugin "missing"`) {
		t.Errorf("got %v", err)
	}
}

func TestValidateConfigUnknownConnection(t *testing.T) {
	s := &Server{Config: newCfg()}
	s.Config.Routes = []*Route{
		{Path: "/x", Method: "POST", Type: "stream-publish",
			StreamPublishConfig: &routes.StreamPublishConfig{Connection: "missing"}},
	}
	err := s.ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), `unknown connection "missing"`) {
		t.Errorf("got %v", err)
	}
}

func TestValidateConfigBadPluginTransport(t *testing.T) {
	s := &Server{Config: &Config{
		Plugins: map[string]*plugins.PluginConfig{"x": {Transport: "process"}}, // missing command
	}}
	if err := s.ValidateConfig(); err == nil {
		t.Error("expected error for plugin missing command")
	}
}

func TestValidateConfigBadConnectionMissingPath(t *testing.T) {
	s := &Server{Config: &Config{
		Connections: map[string]*connections.ConnectionConfig{"x": {Type: "sse"}}, // missing path
	}}
	if err := s.ValidateConfig(); err == nil {
		t.Error("expected error for connection missing subscribe_path")
	}
}

func TestValidateConfigLimitsRegistryWellFormed(t *testing.T) {
	s := &Server{Config: &Config{
		Limits: map[string]*LimitEntry{
			"size_5mb": {Case: CaseBodyTooLarge, MaxSize: "5MB"},
			"rate_100": {Case: CaseRateLimited, RPS: 100},
		},
	}}
	if err := s.ValidateConfig(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateConfigLimitsRejectsMissingCase(t *testing.T) {
	s := &Server{Config: &Config{
		Limits: map[string]*LimitEntry{
			"oops": {MaxSize: "5MB"}, // no Case
		},
	}}
	err := s.ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), `limits["oops"]: missing case`) {
		t.Errorf("got %v", err)
	}
}

func TestValidateConfigLimitsRejectsUnknownCase(t *testing.T) {
	s := &Server{Config: &Config{
		Limits: map[string]*LimitEntry{
			"weird": {Case: "bogus_case"},
		},
	}}
	err := s.ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), `unknown case "bogus_case"`) {
		t.Errorf("got %v", err)
	}
}

func TestValidateConfigRouteRefsResolveAgainstRegistry(t *testing.T) {
	cfg := newCfg()
	cfg.Limits = map[string]*LimitEntry{
		"rate_100": {Case: CaseRateLimited, RPS: 100},
	}
	cfg.Routes = []*Route{
		{Path: "/api/items", Method: "GET", Type: "plugin",
			PluginConfig: &routes.PluginConfig{Name: "echo"},
			Limits:       []string{"rate_100"}}, // resolves
	}
	s := &Server{Config: cfg}
	if err := s.ValidateConfig(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateConfigRouteRefsRejectMissingName(t *testing.T) {
	cfg := newCfg()
	cfg.Limits = map[string]*LimitEntry{
		"rate_100": {Case: CaseRateLimited, RPS: 100},
	}
	cfg.Routes = []*Route{
		{Path: "/api/items", Method: "GET", Type: "plugin",
			PluginConfig: &routes.PluginConfig{Name: "echo"},
			Limits:       []string{"rate_999"}}, // unknown
	}
	s := &Server{Config: cfg}
	err := s.ValidateConfig()
	if err == nil || !strings.Contains(err.Error(), `unknown limit "rate_999"`) {
		t.Errorf("got %v", err)
	}
}
