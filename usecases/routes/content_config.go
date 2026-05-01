package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ContentConfig struct {
	StatusCode          *int        `yaml:"status_code"`
	Headers             [][2]string `yaml:"headers"`
	Body                string      `yaml:"body"`
	PrintRequest        bool        `yaml:"print_request"`
	TimeoutMilliseconds int         `yaml:"timeout_milliseconds"`
}

// CreateRoute implements servers.RouteConfig.
func (c *ContentConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	if c.Body == "" {
		return nil, fmt.Errorf("missing body")
	}

	statusCode := 200
	if c.StatusCode != nil {
		statusCode = *c.StatusCode
	}

	return func(w http.ResponseWriter, r *http.Request) {

		if c.PrintRequest {
			printRequest(r)
		}

		if c.TimeoutMilliseconds > 0 {
			time.Sleep(time.Duration(c.TimeoutMilliseconds) * time.Millisecond)
		}

		for _, item := range c.Headers {
			w.Header().Add(item[0], item[1])
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(c.Body))

	}, nil
}

func (c *ContentConfig) Validate() error {
	if c.Body == "" {
		return fmt.Errorf("body is required")
	}
	if c.StatusCode != nil && (*c.StatusCode < 100 || *c.StatusCode > 599) {
		return fmt.Errorf("invalid status code: %d", *c.StatusCode)
	}
	return nil
}

func printRequest(r *http.Request) {
	var sb strings.Builder

	// Header
	sb.WriteString("\n")
	sb.WriteString("╔════════════════════════════════════════════════════════════════\n")
	sb.WriteString("║ REQUEST DETAILS\n")
	sb.WriteString("╚════════════════════════════════════════════════════════════════\n\n")

	// Method and URL
	sb.WriteString(fmt.Sprintf("Method: %s\n", r.Method))
	sb.WriteString(fmt.Sprintf("URL:    %s\n", r.URL.String()))
	sb.WriteString(fmt.Sprintf("Path:   %s\n", r.URL.Path))
	sb.WriteString(fmt.Sprintf("Host:   %s\n", r.Host))
	sb.WriteString(fmt.Sprintf("Remote: %s\n", r.RemoteAddr))
	sb.WriteString("\n")

	// Headers
	if len(r.Header) > 0 {
		sb.WriteString("┌─ HEADERS ─────────────────────────────────────────────────────\n")
		for key, values := range r.Header {
			for _, value := range values {
				sb.WriteString(fmt.Sprintf("│ %s: %s\n", key, value))
			}
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")
	}

	// Query Parameters
	if len(r.URL.Query()) > 0 {
		sb.WriteString("┌─ QUERY PARAMETERS ────────────────────────────────────────────\n")
		for key, values := range r.URL.Query() {
			for _, value := range values {
				sb.WriteString(fmt.Sprintf("│ %s = %s\n", key, value))
			}
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")
	}

	// Body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sb.WriteString(fmt.Sprintf("Error reading body: %v\n", err))
	} else {
		// Restore the body for downstream handlers
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		if len(body) > 0 {
			sb.WriteString("┌─ BODY ────────────────────────────────────────────────────────\n")

			contentType := r.Header.Get("Content-Type")
			isJSON := strings.Contains(strings.ToLower(contentType), "application/json") ||
				(len(body) > 0 && body[0] == '{') ||
				(len(body) > 0 && body[0] == '[')

			if isJSON {
				var prettyJSON bytes.Buffer
				if err := json.Indent(&prettyJSON, body, "│ ", "  "); err != nil {
					// If prettification fails, print raw
					sb.WriteString(fmt.Sprintf("│ %s\n", string(body)))
				} else {
					sb.WriteString(prettyJSON.String())
					sb.WriteString("\n")
				}
			} else {
				// For non-JSON, print raw body (handle multiline)
				lines := strings.Split(string(body), "\n")
				for _, line := range lines {
					sb.WriteString(fmt.Sprintf("│ %s\n", line))
				}
			}

			sb.WriteString("└───────────────────────────────────────────────────────────────\n")
		} else {
			sb.WriteString("(empty body)\n")
		}
	}

	sb.WriteString("\n")

	// Print everything at once
	fmt.Print(sb.String())
}
