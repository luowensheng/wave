package servers

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ANSI color codes
const (
	reset     = "\033[0m"
	red       = "\033[31m"
	green     = "\033[32m"
	yellow    = "\033[33m"
	blue      = "\033[34m"
	magenta   = "\033[35m"
	cyan      = "\033[36m"
	white     = "\033[37m"
	brightRed = "\033[91m"
)

// loggingMiddleware logs requests with colors for better dev experience.
// Safe for production if you redirect output (colors won't break JSON parsers if disabled).
func loggingMiddleware(next http.Handler) http.Handler {
	cwd, _ := os.Getwd()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("CWD: ", cwd)
		
		start := time.Now()

		clientIP := getClientIP(r)
		safeURL := redactSensitiveParams(r.URL)
		ww := &responseWrapper{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		duration := time.Since(start)
		durationMs := float64(duration.Microseconds()) / 1000.0

		// Color by HTTP method
		methodColor := methodColor(r.Method)
		methodStr := fmt.Sprintf("%s%s%s", methodColor, r.Method, reset)

		// Color by status code
		statusColor := statusColor(ww.status)
		statusStr := fmt.Sprintf("%s%d%s", statusColor, ww.status, reset)

		// Color duration if slow (>500ms)
		durationStr := fmt.Sprintf("%.2fms", durationMs)
		if durationMs > 500 {
			durationStr = fmt.Sprintf("%s%.2fms%s", brightRed, durationMs, reset)
		} else if durationMs > 100 {
			durationStr = fmt.Sprintf("%s%.2fms%s", yellow, durationMs, reset)
		} else {
			durationStr = fmt.Sprintf("%s%.2fms%s", green, durationMs, reset)
		}

		// Format full log line
		logLine := fmt.Sprintf(
			"[%s] %s %s%s %s → %s | %s | %s | %s",
			time.Now().Format("15:04:05.000"),
			methodStr,
			safeURL.Path,
			formatQuery(safeURL.RawQuery),
			clientIP,
			statusStr,
			fmt.Sprintf("%dB", ww.size),
			durationStr,
			truncate(r.UserAgent(), 40),
		)

		fmt.Println(logLine)
	})
}

// responseWrapper captures status and size
type responseWrapper struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.status = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWrapper) Write(data []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(data)
	rw.size += n
	return n, err
}

func (rw *responseWrapper) Flush() {
	rw.ResponseWriter.(http.Flusher).Flush()
}

// redactSensitiveParams removes secrets from query string
func redactSensitiveParams(u *url.URL) *url.URL {
	if u.RawQuery == "" {
		return u
	}

	sensitive := map[string]bool{
		"password": true, "pass": true, "passwd": true, "secret": true,
		"token": true, "api_key": true, "access_token": true, "refresh_token": true,
		"auth": true, "authorization": true, "pin": true, "ssn": true,
	}

	query := u.Query()
	redacted := false
	for k := range query {
		if sensitive[strings.ToLower(k)] {
			query.Set(k, "[REDACTED]")
			redacted = true
		}
	}

	if !redacted {
		return u
	}

	newURL := *u
	newURL.RawQuery = query.Encode()
	return &newURL
}

// formatQuery adds parentheses and gray color if query exists
func formatQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	return fmt.Sprintf("%s?%s%s", cyan, rawQuery, reset)
}

// methodColor returns ANSI color for HTTP method
func methodColor(method string) string {
	switch method {
	case "GET":
		return blue
	case "POST":
		return green
	case "PUT", "PATCH":
		return yellow
	case "DELETE":
		return red
	default:
		return magenta
	}
}

// statusColor returns ANSI color based on HTTP status
func statusColor(status int) string {
	switch {
	case status >= 500:
		return brightRed
	case status >= 400:
		return red
	case status >= 300:
		return yellow
	case status >= 200:
		return green
	default:
		return white
	}
}

// getClientIP safely extracts client IP (assumes trusted proxy in prod)
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); net.ParseIP(ip) != nil {
			return ip
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" && net.ParseIP(realIP) != nil {
		return realIP
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// truncate shortens long strings
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen <= 3 {
		return s
	}
	return s[:maxLen-3] + "..."
}
