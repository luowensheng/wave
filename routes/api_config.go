package routes

import (
	"bufio"
	"bytes"
	"easyserver/render"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"log"
)

type APIConfig struct {
	Request  *Request  `yaml:"request,omitempty"`
	Response *Response `yaml:"response,omitempty"`
}

type APIHandler struct {
	config *APIConfig
}

// CreateRoute implements servers.RouteConfig.
func (c *APIConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	handler := &APIHandler{
		config: c,
	}
	return handler.ServeHTTP, nil
}

func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var requestData map[string]any
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Create template functions
	bodyBuf, err := render.Render(h.config.Request.Body, requestData)
	if err != nil {
		log.Printf("Template error : %v", err)
		return
	}

	// Create upstream request
	upstreamURL, err := url.Parse(h.config.Request.URL)
	if err != nil {
		http.Error(w, "Invalid upstream URL", http.StatusInternalServerError)
		return
	}

	upstreamReq, err := http.NewRequest(h.config.Request.Method, upstreamURL.String(), bodyBuf)
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	// Set headers
	for _, header := range h.config.Request.Headers {
		if len(header) == 2 && r.Header.Get(header[0]) == "" {
			upstreamReq.Header.Set(header[0], header[1])
		}
	}

	// Copy original request headers (optional)
	for key, values := range r.Header {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	fmt.Println(upstreamReq.Header)

	// Make upstream request
	client := &http.Client{}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		http.Error(w, "Upstream request failed", http.StatusBadGateway)
		log.Printf("Upstream request error: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("STREAM: ", h.config.Response.Stream)
	// Handle response based on configuration
	if h.config.Response.Stream {
		h.handleStreamResponse(w, resp, h.config.Response)
	} else {
		h.handleRegularResponse(w, resp, h.config.Response)
	}
}

func (h *APIHandler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, rp *Response) {
	w.Header().Set("Content-Type", "application/x-ndjson") // or "application/json; charset=utf-8"
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Optional: for Nginx
	// w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("ERROR: CANNOT STREAM")
		io.Copy(w, resp.Body)
		return
	}
	var builder bytes.Buffer
	var inQuotes bool
	var escaped bool
	var count int
	var startChar byte // '{' or '['
	var endChar byte   // '}' or ']'

	scanner := bufio.NewScanner(resp.Body)

	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		// Handle EOF
		if atEOF {
			if builder.Len() > 0 {
				builder.Write(data)
				content := make([]byte, builder.Len())
				copy(content, builder.Bytes())
				builder.Reset()
				return len(data), content, bufio.ErrFinalToken
			}
			if len(data) > 0 {
				return len(data), data, bufio.ErrFinalToken
			}
			return 0, nil, nil
		}

		for i := 0; i < len(data); i++ {
			c := data[i]

			// Handle escape sequences
			if escaped {
				escaped = false
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}
			if c == '\\' {
				escaped = true
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}

			// Toggle quote state
			if c == '"' {
				inQuotes = !inQuotes
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}

			// If inside quotes, just buffer
			if inQuotes {
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}

			// Detect start character on first non-whitespace
			if count == 0 && (c == '{' || c == '[') {
				startChar = c
				if c == '{' {
					endChar = '}'
				} else {
					endChar = ']'
				}
				count = 1

				// If we have buffered data, this is a new object
				if builder.Len() > 0 {
					content := make([]byte, builder.Len())
					copy(content, builder.Bytes())
					builder.Reset()
					builder.WriteByte(c)
					return i + 1, content, nil
				}
				builder.WriteByte(c)
				continue
			}

			// Track nesting and buffer everything
			if count > 0 {
				builder.WriteByte(c)

				switch c {
				case startChar:
					count++
				case endChar:
					count--

					// Complete JSON object/array found
					if count == 0 {
						content := make([]byte, builder.Len())
						copy(content, builder.Bytes())
						builder.Reset()
						return i + 1, content, nil
					}
				}
			}
		}

		// Need more data
		return len(data), nil, nil
	})

	var counter = 0
	for scanner.Scan() {
		line := scanner.Bytes()
		counter += 1
		if len(line) == 0 {
			continue
		}

		if rp.Transform == "json" {
			reader := NewJSONReader(line)
			output := make(map[string]any)
			hasValidData := false

			for k, vPath := range rp.Output {
				v, err := reader.Get(vPath)
				if err != nil {
					continue
				}
				var value any
				if err := v.Unmarshal(&value); err != nil {
					continue
				}
				output[k] = value
				hasValidData = true
			}

			if hasValidData {
				// Marshal manually
				outBytes, err := json.Marshal(output)
				if err != nil {
					log.Printf("JSON marshal error: %v", err)
					continue
				}

				w.Write(outBytes)
				w.Write([]byte("\n")) // NDJSON format: one JSON per line
				flusher.Flush()       // ⚠️ MUST flush here
			} else {
				fmt.Println("INVALID DATA!!!: ", string(line))
			}
		} else {
			w.Write(line)
			w.Write([]byte("\n"))
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Scanner error: %v", err)
	}
}

func (h *APIHandler) handleRegularResponse(w http.ResponseWriter, resp *http.Response, rp *Response) {
	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if h.config.Response.Transform != "json" {
		io.Copy(w, resp.Body)
		return
	}

	bytes, err := io.ReadAll(resp.Request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	reader := NewJSONReader(bytes)
	output := make(map[string]interface{})
	hasValidData := false

	for k, vPath := range rp.Output {
		v, err := reader.Get(vPath)
		if err != nil {
			log.Printf("Path %s not found: %v", vPath, err)
			continue
		}
		var value any
		if err := v.Unmarshal(&value); err != nil {
			continue
		}
		output[k] = value
		hasValidData = true
	}
	if hasValidData {
		outBytes, err := json.Marshal(output)
		if err != nil {
			log.Printf("JSON marshal error: %v", err)
			return
		}
		w.Write(outBytes)
	}
}
