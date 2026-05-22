package servers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/luowensheng/wave/infra/oidc"

	"gopkg.in/yaml.v3"
)

// CheckResult is one diagnostic finding.
type CheckResult struct {
	Name    string
	Status  string // "ok" | "warn" | "fail"
	Message string
}

// RunDoctor performs the same structural validation as ValidateConfig
// PLUS live connectivity checks against external systems referenced in
// the config — HTTP plugin endpoints, OIDC issuers, SQLite paths,
// subprocess plugin command existence. Returns the full set of findings
// and the count of failures.
//
// Designed for human consumption (`wave doctor`) and CI gates
// alike; failures move it to non-zero exit, warnings don't.
func (s *Server) RunDoctor(ctx context.Context) ([]CheckResult, int) {
	var out []CheckResult
	add := func(name, status, msg string) {
		out = append(out, CheckResult{Name: name, Status: status, Message: msg})
	}

	// 1. Structural validation (reuses ValidateConfig).
	if err := s.ValidateConfig(); err != nil {
		add("config", "fail", err.Error())
	} else {
		add("config", "ok", fmt.Sprintf("%d routes, %d plugins, %d connections",
			len(s.Config.Routes), len(s.Config.Plugins), len(s.Config.Connections)))
	}

	// Materialize routes for further inspection (validate did this for us
	// but we want it in any path).
	if len(s.Config.Routes) == 0 {
		if b, err := s.Config.RawRoutes.Bytes(); err == nil && len(b) > 0 {
			_ = yaml.Unmarshal(b, &s.Config.Routes)
		}
	}

	// 2. Plugin transport-specific checks.
	for name, p := range s.Config.Plugins {
		switch p.Transport {
		case "process":
			path := strings.Fields(p.Command)
			if len(path) == 0 {
				add("plugin:"+name, "fail", "empty command")
				continue
			}
			if _, err := os.Stat(path[0]); err != nil {
				add("plugin:"+name, "fail", "command not found: "+path[0])
			} else {
				add("plugin:"+name, "ok", "command exists: "+path[0])
			}
		case "http":
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.Address, nil)
			resp, err := http.DefaultClient.Do(req)
			cancel()
			if err != nil {
				add("plugin:"+name, "warn", "address unreachable: "+err.Error())
				break
			}
			resp.Body.Close()
			add("plugin:"+name, "ok", fmt.Sprintf("address reachable (status %d)", resp.StatusCode))
		case "grpc", "wasm":
			add("plugin:"+name, "warn", "transport stubbed in this build (returns ErrNotImplemented at call time)")
		}
	}

	// 3. OIDC issuer reachability — verify discovery + JWKS load.
	for name, ac := range s.Config.Auth {
		if !strings.EqualFold(ac.Type, "oidc") {
			continue
		}
		if ac.Issuer == "" || ac.ClientID == "" {
			add("auth:"+name, "fail", "oidc requires issuer + client_id")
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := oidc.New(ctx, oidc.Config{Issuer: ac.Issuer, ClientID: ac.ClientID})
		cancel()
		if err != nil {
			add("auth:"+name, "fail", "OIDC discovery failed: "+err.Error())
		} else {
			add("auth:"+name, "ok", "OIDC discovery + JWKS load succeeded")
		}
	}

	// 4. SQLite storage — try to open + ping.
	for name, st := range s.Config.Storage {
		if st == nil {
			continue
		}
		// We don't depend on the storage package types here (would create
		// an import cycle in some configurations); reflect on the YAML
		// tag-named fields via a minimal map round-trip.
		raw, _ := yaml.Marshal(st)
		var as struct {
			Type string `yaml:"type"`
			Path string `yaml:"path"`
		}
		_ = yaml.Unmarshal(raw, &as)
		if !strings.EqualFold(as.Type, "sqlite") || as.Path == "" {
			continue
		}
		db, err := sql.Open("sqlite3", as.Path)
		if err != nil {
			add("storage:"+name, "fail", "open: "+err.Error())
			continue
		}
		ctxPing, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(ctxPing)
		cancel()
		_ = db.Close()
		if err != nil {
			add("storage:"+name, "fail", "ping: "+err.Error())
		} else {
			add("storage:"+name, "ok", "sqlite reachable: "+as.Path)
		}
	}

	// 5. File-based routes — confirm referenced files exist.
	for _, route := range s.Config.Routes {
		if route.FileConfig != nil && route.FileConfig.FilePath != "" {
			if _, err := os.Stat(route.FileConfig.FilePath); err != nil {
				add("route:"+route.Path, "warn", "file not found: "+route.FileConfig.FilePath)
			}
		}
		if route.StaticDirConfig != nil && route.StaticDirConfig.Dir != "" {
			if info, err := os.Stat(route.StaticDirConfig.Dir); err != nil || !info.IsDir() {
				add("route:"+route.Path, "warn", "static dir missing: "+route.StaticDirConfig.Dir)
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	failures := 0
	for _, c := range out {
		if c.Status == "fail" {
			failures++
		}
	}
	return out, failures
}
