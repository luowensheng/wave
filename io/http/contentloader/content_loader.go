package contentloader

import (
	"github.com/luowensheng/wave/orchestrator/features/auth"
	"github.com/luowensheng/wave/domain"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type File = domain.File
type Reader = domain.FileReader

type ContentLoader interface {
	GetValue(name string) (any, error)
	GetFile(name string) (*File, error)
	GetValues() map[string]any
}

type JSONContentLoader struct {
	data map[string]any
}

// GetFile implements ContentLoader.
func (j *JSONContentLoader) GetFile(name string) (*File, error) {
	// fileData, err := j.GetValue(name)
	// if err != nil {
	// 	return nil, err
	// }
	return &File{}, nil
}

// GetValue implements ContentLoader.
func (j *JSONContentLoader) GetValue(name string) (any, error) {
	value, found := j.data[name]
	if !found {
		return nil, fmt.Errorf("missing value: %s", name)
	}
	return value, nil
}

// GetValues implements ContentLoader.
func (j *JSONContentLoader) GetValues() map[string]any {
	return j.data
}

type FormDataContentLoader struct {
	form   url.Values
	files  map[string][]*multipart.FileHeader
	reader *multipart.Reader
}

// func ()  {

// }

type MultipartReader struct {
	file *multipart.FileHeader
}

type LocalFileReader struct {
	Path string
}

func (f *LocalFileReader) Open() (io.ReadCloser, error) {
	return os.Open(f.Path)
}

func (f *MultipartReader) Open() (io.ReadCloser, error) {
	return f.file.Open()
}

// GetFile implements ContentLoader.
func (f *FormDataContentLoader) GetFile(name string) (*File, error) {
	files, found := f.files[name]
	if !found || len(files) == 0 {
		return nil, fmt.Errorf("file not found: %s", name)
	}

	// Return the first file if multiple files with same name
	file := files[0]
	// file.
	// fileReader, err := file.Open()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to open file %s: %v", name, err)
	// }
	// defer fileReader.Close()

	// // Read file content
	// content, err := io.ReadAll(fileReader)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read file %s: %v", name, err)
	// }

	return &File{
		Filename: file.Filename,
		Size:     file.Size,
		Reader:   &MultipartReader{file: file},
	}, nil
}

// GetValue implements ContentLoader.
func (f *FormDataContentLoader) GetValue(name string) (any, error) {
	values, found := f.form[name]
	if !found || len(values) == 0 {
		return nil, fmt.Errorf("missing value: %s", name)
	}

	// Return single value if only one, otherwise return slice
	if len(values) == 1 {
		return values[0], nil
	}
	return values, nil
}

// GetValues implements ContentLoader.
func (f *FormDataContentLoader) GetValues() map[string]any {
	result := make(map[string]any)

	// Add form values
	for key, values := range f.form {
		if len(values) == 1 {
			result[key] = values[0]
		} else {
			result[key] = values
		}
	}

	// Add file information
	for key, files := range f.files {
		fileInfos := make([]map[string]any, len(files))
		for i, file := range files {
			fileInfos[i] = map[string]any{
				"filename": file.Filename,
				"size":     file.Size,
				"header":   file.Header,
			}
		}

		if len(fileInfos) == 1 {
			result[key] = fileInfos[0]
		} else {
			result[key] = fileInfos
		}
	}

	return result
}

type URLFormContentLoader struct {
	form url.Values
}

// GetFile implements ContentLoader.
func (u *URLFormContentLoader) GetFile(name string) (*File, error) {
	return nil, fmt.Errorf("URL-encoded forms do not support files")
}

// GetValue implements ContentLoader.
func (u *URLFormContentLoader) GetValue(name string) (any, error) {
	values, found := u.form[name]
	if !found || len(values) == 0 {
		return nil, fmt.Errorf("missing value: %s", name)
	}

	// Return single value if only one, otherwise return slice
	if len(values) == 1 {
		return values[0], nil
	}
	return values, nil
}

// GetValues implements ContentLoader.
func (u *URLFormContentLoader) GetValues() map[string]any {
	result := make(map[string]any)

	for key, values := range u.form {
		if len(values) == 1 {
			result[key] = values[0]
		} else {
			result[key] = values
		}
	}

	return result
}

func NewJSONContentLoader(r *http.Request) (*JSONContentLoader, error) {
	var data map[string]any

	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}

	// Validate decoded data is not nil
	if data == nil {
		return nil, fmt.Errorf("request body cannot be empty")
	}

	return &JSONContentLoader{data: data}, nil
}

func NewFormDataContentLoader(r *http.Request) (*FormDataContentLoader, error) {
	err := r.ParseMultipartForm(32 << 20) // 32 MB max memory
	if err != nil {
		return nil, fmt.Errorf("failed to parse multipart form: %v", err)
	}

	return &FormDataContentLoader{
		form:  r.MultipartForm.Value,
		files: r.MultipartForm.File,
	}, nil
}

func NewURLFormContentLoader(r *http.Request) (*URLFormContentLoader, error) {
	err := r.ParseForm()
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL-encoded form: %v", err)
	}

	// fmt.Println("URL FORM VARS: ", r.Form.Encode())
	// fmt.Println("URL PATH: ", r.URL.Path)
	// fmt.Println("URL RawPath: ", r.URL.RawPath)
	// fmt.Println("URL URL VARS: ", r.URL.Query().Encode())

	return &URLFormContentLoader{form: r.Form}, nil
}

func GetDataLoader(expectedContentType string, r *http.Request) (*DataLoader, error) {
	// Validate content type for methods that expect specific content type

	expectedContentType = strings.ToLower(expectedContentType)
	if expectedContentType == "" {
		expectedContentType = "application/x-www-form-urlencoded"
	}

	contentType := strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Type")))
	if contentType == "" {
		contentType = "application/x-www-form-urlencoded"
	}

	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])

	// common.PrintJSON(common.Object{
	// 	"expectedContentType": expectedContentType,
	// 	"contentType":         contentType,
	// })

	if !strings.Contains(contentType, expectedContentType) {
		return nil, fmt.Errorf("content-type must be %s", expectedContentType)
	}

	var loader ContentLoader
	var err error
	switch expectedContentType {
	case "application/json":
		loader, err = NewJSONContentLoader(r)

	case "multipart/form-data":
		loader, err = NewFormDataContentLoader(r)

	case "application/x-www-form-urlencoded":
		loader, err = NewURLFormContentLoader(r)
	}

	var errorMessage string
	if err == nil {
		return &DataLoader{r: r, loader: loader}, nil
	} else {
		errorMessage = err.Error()
	}

	return nil, fmt.Errorf("unsupported content-type: %s -> ERR: %s", expectedContentType, errorMessage)
}

type DataLoader struct {
	r      *http.Request
	loader ContentLoader
}

// GetFile implements ContentLoader.
func (d *DataLoader) GetFile(name string) (*File, error) {
	return d.loader.GetFile(name)
}

type KEY string

// GetFile implements ContentLoader.
func (d *DataLoader) GetUser() (*auth.PublicUser, error) {
	// var user User
	value := d.r.Context().Value(auth.UserContextKey)
	user, ok := value.(*auth.PublicUser)
	// common.PrintJSON(common.Object{
	// 	"auth_user": user,
	// })
	if !ok {
		return nil, fmt.Errorf("missing %s key", auth.UserContextKey)
	}
	return user, nil
}

// // GetFile implements ContentLoader.
// func (d *DataLoader) SetUser(user *User) {
// 	ctx := context.WithValue(d.r.Context(), auth.UserContextKey, user)
// 	d.r = d.r.WithContext(ctx)
// }

// GetFile implements ContentLoader.
func (d *DataLoader) GetPathVar(name string) string {
	return d.r.PathValue(name)
}
func (d *DataLoader) Query(name string) string {
	return d.r.URL.Query().Get(name)
}

// GetValue implements ContentLoader.
func (d *DataLoader) GetValue(name string) (any, error) {
	value := strings.TrimSpace(d.r.PathValue(name))
	if value != "" {
		return value, nil
	}

	return d.loader.GetValue(name)
}

// GetValues implements ContentLoader.
func (d *DataLoader) GetValues() map[string]any {

	return d.loader.GetValues()
}

// func (d *DataLoader) Print() {
// 	mapping := d.loader.GetValues()
// 	common.PrintJSON(mapping)
// }

// Helper function to determine content loader based on request content type
func GetContentLoaderFromRequest(r *http.Request) (*DataLoader, error) {
	contentType := strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Type")))
	var loader ContentLoader
	var err error
	if strings.Contains(contentType, "application/json") {
		loader, err = NewJSONContentLoader(r)
	} else if strings.Contains(contentType, "multipart/form-data") {
		loader, err = NewFormDataContentLoader(r)
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		loader, err = NewURLFormContentLoader(r)
	}

	if err == nil {
		return &DataLoader{r: r, loader: loader}, nil
	}

	return nil, fmt.Errorf("unsupported content-type: %s", contentType)
}
