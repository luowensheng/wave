package storage_access

import (
	"wave/infra/inputs"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"

	"wave/infra/render"
	"wave/io/http/contentloader"
)

type Config struct {
	Source              string `yaml:"source"`
	Execute             string `yaml:"execute"`
	OutputTemplate      string `yaml:"output_template"`
	ResponseContentType string `yaml:"response_content_type"`
	ExpectedContentType string `yaml:"expected_content_type"`
}

// StorageRef is the interface that storage backends must satisfy.
type StorageRef interface {
	Execute(command string, data *contentloader.DataLoader) (any, error)
}

// GetStorageFn is a function that retrieves a named storage backend.
// Inject this before using Config.CreateRoute.
var GetStorageFn func(name string) (StorageRef, bool)

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if c.Source == "" {
		return nil, fmt.Errorf("route source cannot be empty for path: %s", path)
	}

	if c.Execute == "" {
		return nil, fmt.Errorf("route execute cannot be empty for path: %s", path)
	}

	if c.OutputTemplate == "" && c.ResponseContentType != "$filetype" {
		return nil, fmt.Errorf("route output_template cannot be empty for path: %s", path)
	}

	if GetStorageFn == nil {
		return nil, fmt.Errorf("storage not configured")
	}

	storage, found := GetStorageFn(c.Source)
	if !found {
		return nil, fmt.Errorf("undefined source: '%s'", c.Source)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var dl *contentloader.DataLoader
		var err error
		var expectedContentType = c.ExpectedContentType

		// Strict-scope path: when the route declared `inputs:`, the
		// validated map is on the context. Build a DataLoader that
		// exposes ONLY those keys so the SQL template can't reference
		// anything outside the declared contract.
		if v := inputs.FromContext(r.Context()); len(v) > 0 {
			dl = contentloader.NewDataLoaderFromContentLoader(r,
				contentloader.NewInputsLoader(v))
		} else {
			switch method {
			case "POST", "PUT", "PATCH":
				if r.Body == nil {
					http.Error(w, "request body is required", http.StatusBadRequest)
					return
				}
				dl, err = contentloader.GetDataLoader(expectedContentType, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			default:
				dl, err = contentloader.GetDataLoader("application/x-www-form-urlencoded", r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
		}

		result, err := storage.Execute(c.Execute, dl)
		if err != nil {
			log.Printf("storage execution error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if c.ResponseContentType == "$filetype" {
			dataMap, ok := result.(map[string]any)
			if !ok {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			var f *contentloader.File

			for _, value := range dataMap {
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
