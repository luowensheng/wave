package process

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"easyserver/render"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

// getRunScriptCMD creates a platform-specific command to execute a script
func getRunScriptCMD(script string, execArgs []string) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	if len(execArgs) > 0 {
		input := map[string]any{
			"script": script,
		}
		args := []string{}
		for _, arg := range execArgs {
			buffer, err := render.Render(arg, input)
			if err != nil {
				return nil, err
			}
			args = append(args, buffer.String())

		}
		return exec.Command(args[0], args[1:]...), nil
		
	}

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/C", script)
	default:
		cmd = exec.Command("sh", "-c", script)
	}

	return cmd, nil
}

// Listener interface for HTTP response writing
type Listener interface {
	http.ResponseWriter
}

// Reply handles formatted output from the subprocess
type Reply struct {
	prefix        string
	listener      Listener
	flusher       http.Flusher
	headerWritten bool
}

// NewReply creates a new Reply instance
func NewReply(prefix string, listener Listener) *Reply {
	flusher, _ := listener.(http.Flusher)
	return &Reply{
		prefix:        prefix,
		listener:      listener,
		flusher:       flusher,
		headerWritten: false,
	}
}

// Write processes output from the subprocess and sends HTTP responses
func (r *Reply) Write(p []byte) (n int, err error) {
	if r.listener == nil || len(p) == 0 {
		return len(p), nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(p))
	// Increase buffer size for large payloads
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max token size

	for scanner.Scan() {
		line := scanner.Bytes()

		// Check if line contains our prefix
		if !bytes.Contains(line, []byte(r.prefix)) {
			// Prefix not found: log to stderr
			// fmt.Fprintf(os.Stderr, "[debug] %s\n", line)
			fmt.Print(string(line))
			continue
		}

		// Process line with length-prefixed format
		r.processLine(line)
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	// Ensure headers are written if not already done
	if !r.headerWritten {
		r.listener.WriteHeader(http.StatusOK)
		r.headerWritten = true
	}

	return len(p), nil
}

// processLine handles a line with format: prefix:action|length|content
func (r *Reply) processLine(line []byte) {
	lineStr := string(line)
	prefix := r.prefix + ":"

	// Find all occurrences of our prefix in the line
	startIdx := 0
	for {
		idx := strings.Index(lineStr[startIdx:], prefix)
		if idx < 0 {
			fmt.Println(string(line))
			break
		}

		actualIdx := startIdx + idx
		startIdx = actualIdx + len(prefix)

		// Extract the command starting after the prefix
		// Format: "action|length|content"
		remaining := lineStr[startIdx:]

		if err := r.parseCommand(remaining); err != nil {
			// fmt.Fprintf(os.Stderr, "[%s] Parse error: %v\n", r.prefix, err)
			fmt.Println(string(line))
		}

		// Move to end since we process one command per line with this protocol
		break
	}
}

// parseCommand parses and executes a command in format: action|length|content
func (r *Reply) parseCommand(remaining string) error {
	// Find the action name (everything before first |)
	pipeIdx := strings.Index(remaining, "|")
	if pipeIdx < 0 {
		return fmt.Errorf("missing length delimiter: %s", remaining)
	}

	action := strings.TrimSpace(remaining[:pipeIdx])
	remaining = remaining[pipeIdx+1:]

	// Find the length (between first | and second |)
	pipeIdx = strings.Index(remaining, "|")
	if pipeIdx < 0 {
		return fmt.Errorf("missing content delimiter: %s", remaining)
	}

	lengthStr := remaining[:pipeIdx]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return fmt.Errorf("invalid length '%s': %v", lengthStr, err)
	}

	if length < 0 {
		return fmt.Errorf("negative length: %d", length)
	}

	// Extract exactly 'length' bytes of content
	contentStart := pipeIdx + 1
	if contentStart+length > len(remaining) {
		// Content is truncated, use what we have
		length = len(remaining) - contentStart
	}

	content := []byte(remaining[contentStart : contentStart+length])

	// Process the command
	r.processCommand(action, content)

	return nil
}

// processCommand executes a single command
func (r *Reply) processCommand(action string, data []byte) {
	switch action {
	case "headers":
		if err := r.handleHeaders(data); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Headers error: %v\n", r.prefix, err)
		}

	case "body":
		if !r.headerWritten {
			r.listener.WriteHeader(http.StatusOK)
			r.headerWritten = true
		}
		if len(data) > 0 {
			r.listener.Write(data)
			if r.flusher != nil {
				r.flusher.Flush()
			}
		}

	case "status":
		if err := r.handleStatus(data); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Status error: %v\n", r.prefix, err)
		}

	default:
		if action != "" {
			fmt.Fprintf(os.Stderr, "[%s] Unknown action: [%s] with data length: [%d]\n", r.prefix, action, len(data))
		}
	}
}

// handleHeaders processes multiple headers (format: "Header-Name: value\nHeader2: value2")
func (r *Reply) handleHeaders(data []byte) error {
	if r.headerWritten {
		return fmt.Errorf("headers already sent")
	}

	if len(data) == 0 {
		return nil
	}

	// Support both single header and multiple headers separated by newlines
	lines := bytes.SplitSeq(data, []byte("\n"))

	for line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		parts := bytes.SplitN(line, []byte(":"), 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "[%s] Invalid header format: %s\n", r.prefix, line)
			continue
		}

		headerName := strings.TrimSpace(string(parts[0]))
		headerValue := strings.TrimSpace(string(parts[1]))

		if headerName == "" {
			continue
		}

		r.listener.Header().Add(headerName, headerValue)
	}

	return nil
}

// handleStatus processes status code directives
func (r *Reply) handleStatus(data []byte) error {
	if r.headerWritten {
		return fmt.Errorf("headers already sent")
	}

	statusStr := strings.TrimSpace(string(data))
	if statusStr == "" {
		return nil
	}

	statusCode, err := strconv.Atoi(statusStr)
	if err != nil {
		return fmt.Errorf("invalid status code: %s", statusStr)
	}

	if statusCode < 100 || statusCode > 599 {
		return fmt.Errorf("status code out of range: %d", statusCode)
	}

	r.listener.WriteHeader(statusCode)
	r.headerWritten = true
	return nil
}

// ProcessContext holds the configuration for process execution
type ProcessContext struct {
	Script              string `json:"script"`
	Dir                 string `json:"dir"`
	ReadBody            bool
	Render              bool
	EnhancedMode        bool
	ExecArgs            []string `yaml:"exec_args"`
	ResponseContentType string
}

func (p *ProcessContext) Validate() error {
	_, err := p.RenderScript(map[string]any{})
	return err
}

func (p *ProcessContext) RenderScript(data map[string]any) (string, error) {
	if !p.Render {
		return p.Script, nil
	}
	templ, err := template.New(".").Funcs(funcMap).Parse(p.Script)
	if err != nil {
		return "", err
	}
	w := strings.Builder{}
	err = templ.Execute(&w, map[string]any{"request": data})
	if err != nil {
		return "", err
	}

	return w.String(), nil
}

// HandleRequest executes a script as a subprocess and handles HTTP request/response
func HandleRequest(p *ProcessContext, w http.ResponseWriter, r *http.Request) error {

	// Prepare environment variables
	envVars := []string{
		fmt.Sprintf("REQUEST_METHOD=%s", r.Method),
		fmt.Sprintf("REQUEST_URI=%s", r.URL.Path),
		fmt.Sprintf("QUERY_STRING=%s", r.URL.RawQuery),
		fmt.Sprintf("HTTP_VERSION=%s", r.Proto),
		fmt.Sprintf("REMOTE_ADDR=%s", r.RemoteAddr),
		fmt.Sprintf("HTTP_CONTENT_LENGTH=%d", r.ContentLength),
	}

	// Add HTTP headers as environment variables
	for key, values := range r.Header {
		// Convert to CGI-style header names (HTTP_*)
		envKey := "HTTP_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))

		for _, value := range values {
			value = strings.TrimSpace(value)
			envVars = append(envVars, fmt.Sprintf("%s=%s", envKey, value))
		}
	}

	var input = map[string]any{}
	if p.ReadBody {
		err := json.NewDecoder(r.Body).Decode(&input)
		if err != nil {
			return err

		}
	}
	// Create and configure command

	script, err := p.RenderScript(input)
	if err != nil {
		return err
	}

	script = strings.TrimSpace(script)
	fmt.Printf("\n\nRENDERED SCRIPT\n [%v]:\n'%s'\n\n", p.ExecArgs, script)
	cmd, err := getRunScriptCMD(script, p.ExecArgs)
	if err != nil {
		return err
	}
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdin = r.Body
	if !p.EnhancedMode {
		cmd.Stdout = io.MultiWriter(w, os.Stdout)
	} else {
		prefix := fmt.Sprintf("process:%s", RandString(12))
		reply := NewReply(prefix, w)
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("SEND_STATUS=%s:status", prefix),
			fmt.Sprintf("SEND_HEADERS=%s:headers", prefix),
			fmt.Sprintf("SEND_BODY=%s:body", prefix),
		)

		cmd.Stdout = io.MultiWriter(reply, os.Stdout)
	}
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

var funcMap = template.FuncMap{
	"TrimExt": func(path string) string {
		return strings.TrimSuffix(path, filepath.Ext(path))
	},
	"Format": func(format string, args ...any) string {
		for i, arg := range args {
			if ptr, ok := arg.(*string); ok {
				args[i] = *ptr // Dereference the pointer
			}
		}
		return fmt.Sprintf(format, args...)
	},

	"PathDir": func(path string) string {
		return filepath.Dir(path)
	},

	"AbsPath": func(path string) string {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "~") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return path // Return original path if we can't get home dir
			}
			if path == "~" {
				return homeDir
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(homeDir, path[2:])
			}
			return path // Return as-is if it's ~something (like ~otheruser)
		}
		path, _ = filepath.Abs(path)
		return path
	},
	"PathJoin": func(args ...string) string {
		return filepath.Join(args...)
	},
	"BasePath": func(path string) string {
		return filepath.Base(path)
	},
	"Basename": func(path string) string {
		return filepath.Base(path)
	},
	"FilenameWitoutExt": func(path string) string {
		path = filepath.Base(path)
		return strings.TrimSuffix(path, filepath.Ext(path))
	},
	"OSIsUnix": func() bool {
		return slices.Contains([]string{"darwin", "linux"}, runtime.GOOS)
	},
	"OSIsWindows": func() bool {
		return runtime.GOOS == "windows"
	},
	"OSIsLinux": func() bool {
		return runtime.GOOS == "linux"
	},
	"OSIsDarwin": func() bool {
		return runtime.GOOS == "darwin"
	},
	"OSIsMacOS": func() bool {
		return runtime.GOOS == "darwin"
	},
	"ArchIsAMD64": func() bool {
		return runtime.GOARCH == "amd64"
	},
	"ArchIsARM64": func() bool {
		return runtime.GOARCH == "arm64"
	},
	"MD5": func(content string) string {
		hasher := md5.New()
		hasher.Write([]byte(content))
		context := fmt.Sprintf("%x", hasher.Sum(nil))
		return context
	},
	"InPath": func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	},
	"AnyInPath": func(names ...string) bool {
		for _, name := range names {
			_, err := exec.LookPath(name)
			if err == nil {
				return true
			}
		}

		return false
	},
	"AllInPath": func(names ...string) bool {
		for _, name := range names {
			_, err := exec.LookPath(name)
			if err != nil {
				return false
			}
		}
		return true
	},
	"AllNotInPath": func(names ...string) bool {
		for _, name := range names {
			_, err := exec.LookPath(name)
			if err == nil {
				return false
			}
		}
		return true
	},
	"AnyNotInPath": func(names ...string) bool {
		for _, name := range names {
			_, err := exec.LookPath(name)
			if err != nil {
				return true
			}
		}

		return false
	},
	"SelectFirstInPath": func(names ...string) string {
		for _, name := range names {
			_, err := exec.LookPath(name)
			if err == nil {
				return name
			}
		}
		return ""
	},
	"NotInPath": func(name string) bool {
		_, err := exec.LookPath(name)
		return err != nil
	},

	"IfElse": func(condition bool, trueValue any, falseValue any) any {
		if !condition {
			return falseValue
		}
		return trueValue
	},
	"Capitalize": func(text string) string {
		if text == "" {
			return ""
		}
		runes := []rune(text)
		runes[0] = unicode.ToUpper(runes[0])
		for i := 1; i < len(runes); i++ {
			runes[i] = unicode.ToLower(runes[i])
		}
		return string(runes)
	},
	"CWD": func() string {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return cwd
	},
	"CURRENT_OS": func() string {
		return runtime.GOOS
	},
	"CURRENT_ARCH": func() string {
		return runtime.GOARCH
	},
	"AppDataDir": AppDataDir,
	"CacheDir":   CacheDir,
	"TempDir": func() string {
		return os.TempDir()
	},
	"GetEnv": func(key string) string {
		return os.Getenv(key)
	},
	"EnvOrDefault": func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	},

	"HOME": func() string {
		home, _ := os.UserHomeDir()
		return home
	},
}

func AppDataDir() string {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
	case "darwin":
		baseDir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support")
	default: // linux and other unix-like
		baseDir = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}

	return baseDir
}

func CacheDir() string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin": // macOS
		return home + "/Library/Caches"
	case "linux":
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			return xdg
		}
		return home + "/.cache"
	case "windows":
		if localAppData := os.Getenv("LocalAppData"); localAppData != "" {
			return localAppData
		}
		// fallback
		return home + "\\AppData\\Local"
	default:
		// generic fallback
		dir, _ := os.UserCacheDir()
		return dir
	}
}
