package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"easyserver/infra/common"
	"easyserver/contentloader"
	"strings"
	"text/template"
)

type FilesystemStorageRef struct {
	dir string
}

// Execute implements StorageRef.
func (ref *FilesystemStorageRef) Execute(command string, data *contentloader.DataLoader) (any, error) {

	outputData := map[string]any{}

	common.PrintJSON(common.Object{
		"data.GetValues()": data.GetValues(),
	})

	errors := []error{}

	write := func(file *contentloader.File, outputfilepath string) string {

		outputfilepath = filepath.Join(ref.dir, outputfilepath)
		fileReader, err := file.Reader.Open()
		if err == nil {
			defer fileReader.Close()
			// // Read file content
			content, err := io.ReadAll(fileReader)
			if err == nil {
				err = os.WriteFile(outputfilepath, content, 0777)
				if err == nil {
					return ""
				}
			}
		}
		errors = append(errors, err)
		return err.Error()
	}

	funcMap := template.FuncMap{
		"WRITE": write,
		"UPDATE": func(file *contentloader.File, outputfilepath string) string {
			outputfilepath2 := filepath.Join(ref.dir, outputfilepath)
			_, err := os.Stat(outputfilepath2)
			if err != nil {
				return err.Error()
			}
			return write(file, outputfilepath)
		},
		"getUser": func() any {
			user, err := data.GetUser()
			if err != nil {
				return nil
			}
			errors = append(errors, err)
			return user
		},
		"READ": func(path string, outputKey string) string {

			path = filepath.Join(ref.dir, path)

			info, err := os.Stat(path)
			if err != nil {
				errors = append(errors, err)
				return err.Error()
			}

			outputData[outputKey] = &contentloader.File{
				Size:     info.Size(),
				Filename: info.Name(),
				Reader: &contentloader.LocalFileReader{
					Path: path,
				},
			}
			return ""
		},
		"DELETE": func(path string) string {

			path = filepath.Join(ref.dir, path)

			err := os.Remove(path)
			if err != nil {
				errors = append(errors, err)
			}
			return ""
		},
		"READDIR": func(key string) string {

			entries, err := os.ReadDir(ref.dir)
			if err != nil {
				errors = append(errors, err)
				return ""
			}
			data := []any{}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}

				info, err := entry.Info()
				if err != nil {
					errors = append(errors, err)
					return ""
				}

				size := info.Size()

				data = append(data, common.Object{
					"filename": entry.Name(),
					"size":     size,
				})
			}
			outputData[key] = data
			return ""
		},
		"GET_FILE": func(name string) *contentloader.File {

			file, err := data.GetFile(name)
			if err != nil {
				errors = append(errors, err)
				return nil
			}
			return file
		},
		"GET_VALUE": func(name string) any {

			value, err := data.GetValue(name)
			if err != nil {
				errors = append(errors, err)
				return ""
			}
			return value
		},
		"GET_QUERY": func(name string) any {
			return data.Query(name)
		},
	}

	// Render the template
	tmpl, err := template.New("filesystem").Funcs(funcMap).Parse(command)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filesystem template: %v", err)
	}

	var rendered strings.Builder
	err = tmpl.Execute(&rendered, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to render filesystem template: %v", err)
	}

	return outputData, nil
}

func setupFileSystem(storage *StorageConfig) (StorageRef, error) {

	// Create directory if it doesn't exist
	dir := storage.Path
	if dir != "." && dir != "/" && dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	ref := &FilesystemStorageRef{dir: dir}

	return ref, nil

}
