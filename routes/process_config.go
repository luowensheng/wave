package routes

import (
	"easyserver/pkg/logger"
	"easyserver/process"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type ProcessConfig struct {
	Dir                 string   `yaml:"dir"`
	Script              string   `yaml:"script"`
	SkipRender          bool     `yaml:"skip_render"`
	ExecArgs            []string `yaml:"exec_args"`
	ReadBody            bool     `yaml:"read_body"`
	ResponseContentType string   `yaml:"response_content_type"`
	// EnhancedMode        bool     `yaml:"enhanced_mode"`
	// RawModel bool `yaml:"enhanced_mode"`
	Mode string `yaml:"mode"`
}

// CreateRoute implements servers.RouteConfig.
func (c *ProcessConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	// validation
	dir := c.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			dir = "./"
		}

	} else {
		_, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("%s", err.Error())
		}

	}

	script := strings.TrimSpace(c.Script)
	if script == "" {
		return nil, fmt.Errorf("missing 'script'")
	}
	pc := &process.ProcessContext{
		Script:       script,
		Dir:          dir,
		Render:       !c.SkipRender,
		ExecArgs:     c.ExecArgs,
		ReadBody:     c.ReadBody,
		EnhancedMode: c.Mode != "raw",
		// ResponseContentType: c.ResponseContentType,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error(err)
				http.Error(w, "Unexpected Error", http.StatusInternalServerError)
			}
		}()
		err := process.HandleRequest(pc, w, r)
		if err != nil {
			panic(err)
		}

		if c.ResponseContentType != "" {
			w.Header().Add("content-type", c.ResponseContentType)
		}

	}, nil
}
