package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/luowensheng/wave/infra/inputs"
	"github.com/luowensheng/wave/infra/render"
)

type Config struct {
	Request  *Request  `yaml:"request,omitempty"`
	Response *Response `yaml:"response,omitempty"`
}

type Request struct {
	Type    string      `yaml:"type"`
	Method  string      `yaml:"method"`
	URL     string      `yaml:"url"`
	Headers [][2]string `yaml:"headers"`
	Body    string      `yaml:"body"`
}

type Response struct {
	Transform string            `yaml:"transform"`
	Stream    bool              `yaml:"stream"`
	Output    map[string]string `yaml:"output"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	handler := &apiHandler{config: c}
	return handler.serveHTTP, nil
}

type apiHandler struct {
	config *Config
}

func (h *apiHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Source the template scope. When the route declared `inputs:`,
	// strict-mode kicks in: the template can ONLY reference declared
	// inputs (already parsed + validated by the inputs middleware).
	// Otherwise we fall back to the legacy behavior (whole JSON body
	// → template scope) so existing configs keep working.
	var requestData map[string]any
	if v := inputs.FromContext(r.Context()); len(v) > 0 {
		requestData = v
	} else {
		if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	// Substitute scope into both URL and body templates.
	urlBuf, err := render.Render(h.config.Request.URL, requestData)
	if err != nil {
		log.Printf("URL template error: %v", err)
		http.Error(w, "Invalid upstream URL template", http.StatusInternalServerError)
		return
	}
	bodyBuf, err := render.Render(h.config.Request.Body, requestData)
	if err != nil {
		log.Printf("Template error : %v", err)
		return
	}

	upstreamURL, err := url.Parse(urlBuf.String())
	if err != nil {
		http.Error(w, "Invalid upstream URL", http.StatusInternalServerError)
		return
	}

	upstreamReq, err := http.NewRequest(h.config.Request.Method, upstreamURL.String(), bodyBuf)
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	for _, header := range h.config.Request.Headers {
		if len(header) == 2 && r.Header.Get(header[0]) == "" {
			upstreamReq.Header.Set(header[0], header[1])
		}
	}

	for key, values := range r.Header {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	fmt.Println(upstreamReq.Header)

	client := &http.Client{}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		http.Error(w, "Upstream request failed", http.StatusBadGateway)
		log.Printf("Upstream request error: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("STREAM: ", h.config.Response.Stream)
	if h.config.Response.Stream {
		h.handleStreamResponse(w, resp, h.config.Response)
	} else {
		h.handleRegularResponse(w, resp, h.config.Response)
	}
}

// JSONReader is a minimal JSON path reader used for response transformation.
type JSONReader struct {
	data []byte
}

func newJSONReader(b []byte) *JSONReader {
	return &JSONReader{data: b}
}

func (jr *JSONReader) unmarshal(v any) error {
	return json.Unmarshal(jr.data, v)
}

func (h *apiHandler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, rp *Response) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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
	var startChar byte
	var endChar byte

	scanner := bufio.NewScanner(resp.Body)

	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
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

			if c == '"' {
				inQuotes = !inQuotes
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}

			if inQuotes {
				if count > 0 {
					builder.WriteByte(c)
				}
				continue
			}

			if count == 0 && (c == '{' || c == '[') {
				startChar = c
				if c == '{' {
					endChar = '}'
				} else {
					endChar = ']'
				}
				count = 1

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

			if count > 0 {
				builder.WriteByte(c)

				switch c {
				case startChar:
					count++
				case endChar:
					count--

					if count == 0 {
						content := make([]byte, builder.Len())
						copy(content, builder.Bytes())
						builder.Reset()
						return i + 1, content, nil
					}
				}
			}
		}

		return len(data), nil, nil
	})

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if rp.Transform == "json" {
			var rawLine map[string]json.RawMessage
			if err := json.Unmarshal(line, &rawLine); err != nil {
				fmt.Println("INVALID DATA!!!: ", string(line))
				continue
			}

			output := make(map[string]any)
			hasValidData := false

			for k, vPath := range rp.Output {
				parts := splitPath(vPath)
				val, err := extractFromRaw(rawLine, parts)
				if err != nil {
					continue
				}
				output[k] = val
				hasValidData = true
			}

			if hasValidData {
				outBytes, err := json.Marshal(output)
				if err != nil {
					log.Printf("JSON marshal error: %v", err)
					continue
				}

				w.Write(outBytes)
				w.Write([]byte("\n"))
				flusher.Flush()
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

func (h *apiHandler) handleRegularResponse(w http.ResponseWriter, resp *http.Response, rp *Response) {
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

	b, err := io.ReadAll(resp.Request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	var rawLine map[string]json.RawMessage
	if err := json.Unmarshal(b, &rawLine); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	output := make(map[string]interface{})
	hasValidData := false

	for k, vPath := range rp.Output {
		parts := splitPath(vPath)
		val, err := extractFromRaw(rawLine, parts)
		if err != nil {
			log.Printf("Path %s not found: %v", vPath, err)
			continue
		}
		output[k] = val
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

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '[' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else if c == ']' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func extractFromRaw(data map[string]json.RawMessage, parts []string) (any, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	raw, ok := data[parts[0]]
	if !ok {
		return nil, fmt.Errorf("key %s not found", parts[0])
	}
	if len(parts) == 1 {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, err
	}
	return extractFromRaw(nested, parts[1:])
}
