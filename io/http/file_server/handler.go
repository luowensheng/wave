package file_server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "wave/infra/logger"
	"wave/infra/render"
)

// Config holds the configuration for the file server handler.
// It mirrors usecases/file_server.Config.
type Config struct {
	FileIgnorePatterns []string
	Dir                string
	Prettify           bool
}

func NewHandler(config *Config, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, path)
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		relPath = filepath.Clean(relPath)

		for _, pattern := range config.FileIgnorePatterns {
			if strings.Contains(relPath, pattern) {
				http.NotFound(w, r)
				return
			}
		}

		filePath := filepath.Join(config.Dir, relPath)
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			log.Errorf("Failed to resolve absolute path: %v", err)
			http.NotFound(w, r)
			return
		}

		fmt.Println("looking for: ", absPath)
		absDir, err := filepath.Abs(config.Dir)
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

			content := render.RenderDirectoryIndex(filePath, entries, relPath, config.FileIgnorePatterns)
			w.Write([]byte(content))
		} else {
			if config.Prettify {
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
	}
}
