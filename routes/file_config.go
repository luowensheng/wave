package routes

import (
	"easyserver/auth"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileConfig struct {
	// Path       string `yaml:"path"`
	CatchAll   bool   `json:"catch_all"`
	FilePath   string `yaml:"filepath,omitempty"`
	IsTemplate bool   `yaml:"is_template,omitempty"`
}

func servefile(w http.ResponseWriter, r *http.Request, path string, isTemplate bool) {
	if isTemplate {
		serveTemplateFile(w, r, path)
	} else if strings.HasSuffix(strings.ToLower(path), ".html") {
		serveHTMLWithScriptForSSE(w, r, path)
	} else {

		http.ServeFile(w, r, path)
	}
}

// CreateRoute implements servers.RouteConfig.
func (c *FileConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	if c.FilePath == "" {
		return nil, fmt.Errorf("missing 'filepath' for '%s': ", c.FilePath)
	}

	c.FilePath, _ = filepath.Abs(c.FilePath)

	fmt.Println("looking for: ", c.FilePath)

	// Pre-parse template if IsTemplate is true
	var tmpl *template.Template
	var err error
	if c.IsTemplate {
		tmpl, err = template.ParseFiles(c.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template '%s': %w", c.FilePath, err)
		}
	}

	if c.CatchAll {
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		return func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("looking for: ", c.FilePath)

			if c.IsTemplate {
				serveTemplateWithData(w, r, tmpl, data)
			} else {
				servefile(w, r, c.FilePath, false)
			}
		}, nil

	} else {
		return func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("looking for: ", c.FilePath)

			if path == "/" && r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}

			if c.IsTemplate {
				serveTemplateWithData(w, r, tmpl, data)
			} else {
				servefile(w, r, c.FilePath, false)
			}
		}, nil
	}
}

func (c *FileConfig) Validate() error {
	if c.FilePath == "" {
		return fmt.Errorf("filepath is required")
	}

	// Check if file exists
	if _, err := os.Stat(c.FilePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", c.FilePath)
	}

	// If it's a template, verify it can be parsed
	if c.IsTemplate {
		_, err := template.ParseFiles(c.FilePath)
		if err != nil {
			return fmt.Errorf("invalid template file: %w", err)
		}
	}

	return nil
}

var SSR_SCRIPT_PATH = ""

func serveTemplateFile(w http.ResponseWriter, r *http.Request, filePath string) {
	tmpl, err := template.ParseFiles(filePath)
	if err != nil {
		http.Error(w, "Error parsing template", http.StatusInternalServerError)
		return
	}

	serveTemplateWithData(w, r, tmpl, nil)
}

func serveTemplateWithData(w http.ResponseWriter, r *http.Request, tmpl *template.Template, data map[string]string) {
	// Build template data context
	templateData := buildTemplateData(r, data)

	// Set appropriate headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Execute template
	var buf strings.Builder
	if err := tmpl.Funcs(template.FuncMap{
		"GetUser": func() *auth.PublicUser {
			value := r.Context().Value(auth.UserContextKey)
			user, ok := value.(*auth.PublicUser)
			if !ok {
				panic(fmt.Errorf("missing %s key", auth.UserContextKey))
			}
			return user
		},
	}).Execute(&buf, templateData); err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}

	htmlContent := buf.String()

	// Inject SSR script if needed
	if SSR_SCRIPT_PATH != "" && strings.Contains(strings.ToLower(htmlContent), "</body>") {
		scriptTag := fmt.Sprintf(`<script src="%s"></script>\n`, SSR_SCRIPT_PATH)
		htmlContent = strings.Replace(htmlContent, "</body>", scriptTag+"</body>", 1)
	}

	// Write the response
	w.Write([]byte(htmlContent))
}

// TODO: include functions to load other templates to do other types of handling
func buildTemplateData(r *http.Request, data map[string]string) map[string]interface{} {
	templateData := make(map[string]interface{})

	// Add provided data
	if data != nil {
		for k, v := range data {
			templateData[k] = v
		}
	}

	// Add request context
	templateData["Request"] = map[string]interface{}{
		"Method":     r.Method,
		"URL":        r.URL.String(),
		"Path":       r.URL.Path,
		"Host":       r.Host,
		"RemoteAddr": r.RemoteAddr,
		"UserAgent":  r.UserAgent(),
	}

	// Add query parameters
	query := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	templateData["Query"] = query

	// Add headers
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	templateData["Headers"] = headers

	return templateData
}

func serveHTMLWithScriptForSSE(w http.ResponseWriter, r *http.Request, filePath string) {
	// Read the HTML file
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Convert to string for manipulation
	htmlContent := string(content)

	// Only inject script if there's one provided and HTML has a closing body tag
	if SSR_SCRIPT_PATH != "" && strings.Contains(strings.ToLower(htmlContent), "</body>") {
		// Inject script before closing body tag
		scriptTag := fmt.Sprintf(`<script src="%s"></script>\n`, SSR_SCRIPT_PATH)
		htmlContent = strings.Replace(htmlContent, "</body>", scriptTag+"</body>", 1)
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Serve the modified content
	http.ServeContent(w, r, filepath.Base(filePath), getModTime(filePath), strings.NewReader(htmlContent))
}

// Helper function to get file modification time
func getModTime(filePath string) time.Time {
	if info, err := os.Stat(filePath); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}
