package routes

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type StaticConfig struct {
	Dir                string   `yaml:"dir"`
	FileIgnorePatterns []string `yaml:"file_ignore_patterns,omitempty"`
}

// CreateRoute implements servers.RouteConfig.
func (c *StaticConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	return func(w http.ResponseWriter, r *http.Request) {
		// Determine the requested file path
		relPath := strings.TrimPrefix(r.URL.Path, path)
		for _, pattern := range c.FileIgnorePatterns {
			if strings.Contains(relPath, pattern) {
				http.NotFound(w, r)
				return
			}
		}

		fmt.Println("REL PATH: ", relPath)
		filePath, err := filepath.Abs(filepath.Join(c.Dir, relPath))
		if err != nil {
			fmt.Println("ERRR: ", err.Error())
			http.NotFound(w, r)
			return
		}
		fmt.Println("REL PATH: ", relPath, " => filePath: ", filePath)
		fmt.Println("looking for: ", filePath)

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
	}, nil
}

// https://210.59.192.123/ai-manager/business-card
