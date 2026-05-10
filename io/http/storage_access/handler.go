package storage_access

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"

	"wave/infra/render"
	"wave/io/http/contentloader"
)

// StorageRef is the interface for a storage backend.
type StorageRef interface {
	Execute(command string, data *contentloader.DataLoader) (any, error)
}

// GetStorageFn is a function that retrieves a named storage backend.
type GetStorageFn func(name string) (StorageRef, bool)

// Config holds the configuration for the storage access handler.
// It mirrors usecases/storage_access.Config.
type Config struct {
	Source              string
	Execute             string
	OutputTemplate      string
	ResponseContentType string
	ExpectedContentType string
}

func NewHandler(config *Config, method string, getStorage GetStorageFn) (http.HandlerFunc, error) {
	if config.Source == "" {
		return nil, fmt.Errorf("route source cannot be empty")
	}

	if config.Execute == "" {
		return nil, fmt.Errorf("route execute cannot be empty")
	}

	if config.OutputTemplate == "" && config.ResponseContentType != "$filetype" {
		return nil, fmt.Errorf("route output_template cannot be empty")
	}

	storage, found := getStorage(config.Source)
	if !found {
		return nil, fmt.Errorf("undefined source: '%s'", config.Source)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var data *contentloader.DataLoader
		var err error
		var expectedContentType = config.ExpectedContentType

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
			data, err = contentloader.GetDataLoader("application/x-www-form-urlencoded", r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		result, err := storage.Execute(config.Execute, data)
		if err != nil {
			log.Printf("storage execution error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if config.ResponseContentType == "$filetype" {
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

		if config.ResponseContentType != "" {
			w.Header().Set("Content-Type", config.ResponseContentType)
		}

		buffer, err := render.Render(config.OutputTemplate, result)
		if err != nil {
			log.Printf("template render error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Write(buffer.Bytes())
	}, nil
}
