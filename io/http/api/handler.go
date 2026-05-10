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

	"wave/infra/render"
	"wave/usecases/routes"
)

// Config holds the configuration for the API handler.
// It mirrors usecases/api.Config.
type Config struct {
	Request  *Request
	Response *Response
}

// Request holds the upstream request config.
type Request struct {
	Type    string
	Method  string
	URL     string
	Headers [][2]string
	Body    string
}

// Response holds the upstream response config.
type Response struct {
	Transform string
	Stream    bool
	Output    map[string]string
}

func NewHandler(config *Config) http.HandlerFunc {
	handler := &apiHandler{config: config}
	return handler.serveHTTP
}

type apiHandler struct {
	config *Config
}

func (h *apiHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	var requestData map[string]any
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	bodyBuf, err := render.Render(h.config.Request.Body, requestData)
	if err != nil {
		log.Printf("Template error : %v", err)
		return
	}

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
			reader := routes.NewJSONReader(line)
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
	reader := routes.NewJSONReader(b)
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
