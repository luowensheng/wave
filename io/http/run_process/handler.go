package run_process

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"wave/infra/logger"
	"wave/infra/process"
)

// Config holds the configuration for the run process handler.
// It mirrors usecases/run_process.Config.
type Config struct {
	Dir                 string
	Script              string
	SkipRender          bool
	ExecArgs            []string
	ReadBody            bool
	ResponseContentType string
	Mode                string
}

func NewHandler(config *Config) (http.HandlerFunc, error) {
	dir := config.Dir
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

	script := strings.TrimSpace(config.Script)
	if script == "" {
		return nil, fmt.Errorf("missing 'script'")
	}
	pc := &process.ProcessContext{
		Script:       script,
		Dir:          dir,
		Render:       !config.SkipRender,
		ExecArgs:     config.ExecArgs,
		ReadBody:     config.ReadBody,
		EnhancedMode: config.Mode != "raw",
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

		if config.ResponseContentType != "" {
			w.Header().Add("content-type", config.ResponseContentType)
		}
	}, nil
}
