package routes

import (
	"embed"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed dependencies/*
var dependenciesDir embed.FS

type DependenciesConfig struct{}

// CreateRoute implements servers.RouteConfig.
func (c *DependenciesConfig) CreateRoute(method,path string, data map[string]string) (http.HandlerFunc, error) {

	return func(w http.ResponseWriter, r *http.Request) {
		// Determine the requested file path
		relPath := strings.TrimPrefix(r.URL.Path, path)

		filePath := filepath.Join("dependencies", relPath)
		fmt.Println("LOOKING FOR: ", filePath)

		f, err := dependenciesDir.Open(filePath)
		if err != nil {
			fmt.Println("ERROR: ", err.Error())
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil || stat.IsDir() {
			http.NotFound(w, r)
			return
		}

		// Set Content-Type based on file extension
		ext := filepath.Ext(filePath)
		contentType := mime.TypeByExtension(ext)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		fmt.Printf("filePath='%s'\ncontentType='%s'\n\n\n\n", filePath, contentType)

		// Use ServeContent to enable proper caching with Last-Modified support
		w.Header().Set("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))
		io.Copy(w, f)
	}, nil
}

func (c *DependenciesConfig) Validate() error {
	return nil
}
