package routes

import (
	"easyserver/contentloader"
	"easyserver/render"
	"easyserver/storage"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"

	"log"
)

type StorageAccessConfig struct {
	Source              string `yaml:"source"`
	Execute             string `yaml:"execute"`
	OutputTemplate      string `yaml:"output_template"`
	ResponseContentType string `yaml:"response_content_type"`
	ExpectedContentType string `yaml:"expected_content_type"`
}

// CreateRoute implements servers.RouteConfig.
func (c *StorageAccessConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	// Validate route configuration

	if c.Source == "" {
		return nil, fmt.Errorf("route source cannot be empty for path: %s", path)
	}

	if c.Execute == "" {
		return nil, fmt.Errorf("route execute cannot be empty for path: %s", path)
	}

	if c.OutputTemplate == "" && c.ResponseContentType != "$filetype" {
		return nil, fmt.Errorf("route output_template cannot be empty for path: %s", path)
	}

	storage, found := storage.GetFromStorage(c.Source)
	if !found {
		return nil, fmt.Errorf("undefined source: '%s'", c.Source)
	}
	return func(w http.ResponseWriter, r *http.Request) {

		var data *contentloader.DataLoader
		var err error
		var expectedContentType = c.ExpectedContentType

		switch method {
		case "POST", "PUT", "PATCH":
			if r.Body == nil {
				http.Error(w, "request body is required", http.StatusBadRequest)
				return
			}

			data, err = contentloader.GetDataLoader(expectedContentType, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

		default:
			// For GET/DELETE, extract query parameters if needed
			data, err = contentloader.GetDataLoader("application/x-www-form-urlencoded", r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		result, err := storage.Execute(c.Execute, data)
		if err != nil {
			log.Printf("storage execution error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if c.ResponseContentType == "$filetype" {

			data, ok := result.(map[string]any)
			if !ok {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			// &contentloader.LocalFileReader
			var f *contentloader.File

			for _, value := range data {
				f, ok = value.(*contentloader.File)
				if ok {
					break
				}
			}

			if f == nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			rc, err := f.Reader.Open()
			if err != nil {
				http.Error(w, "Failed to open file", http.StatusInternalServerError)
				return
			}
			defer rc.Close()

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Filename))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", f.Size))
			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(f.Filename)))

			_, err = io.Copy(w, rc)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			return
		}

		if c.ResponseContentType != "" {
			w.Header().Set("Content-Type", c.ResponseContentType)
		}

		buffer, err := render.Render(c.OutputTemplate, result)
		if err != nil {
			log.Printf("template render error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Write(buffer.Bytes())

	}, nil
}
