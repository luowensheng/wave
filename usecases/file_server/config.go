package file_server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/luowensheng/wave/infra/logger"
	"github.com/luowensheng/wave/infra/render"
)

type Config struct {
	FileIgnorePatterns []string `yaml:"file_ignore_patterns,omitempty"`
	Dir                string   `yaml:"dir"`
	Prettify           bool     `yaml:"prettify,omitempty"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, path)
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		relPath = filepath.Clean(relPath)

		for _, pattern := range c.FileIgnorePatterns {
			if strings.Contains(relPath, pattern) {
				http.NotFound(w, r)
				return
			}
		}

		filePath := filepath.Join(c.Dir, relPath)
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			log.Errorf("Failed to resolve absolute path: %v", err)
			http.NotFound(w, r)
			return
		}

		fmt.Println("looking for: ", absPath)
		absDir, err := filepath.Abs(c.Dir)
		if err != nil {
			log.Errorf("Failed to resolve route directory: %v", err)
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) && absPath != absDir {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
			} else {
				log.Errorf("Stat error: %v", err)
				http.NotFound(w, r)
			}
			return
		}

		if info.IsDir() {
			entries, err := os.ReadDir(absPath)
			if err != nil {
				log.Errorf("Failed to read directory: %v", err)
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			content := render.RenderDirectoryIndex(filePath, entries, relPath, c.FileIgnorePatterns)
			w.Write([]byte(content))
		} else {
			if c.Prettify {
				content, err := render.ToHTML(absPath)
				if err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(content))
					return
				}
			}
			http.ServeFile(w, r, absPath)
		}
	}, nil
}

func (c *Config) Validate() error {
	return nil
}
