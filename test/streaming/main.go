package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type Response struct {
	Transform string            `yaml:"transform"`
	Stream    bool              `yaml:"stream"`
	Output    map[string]string `yaml:"output"`
}

// // Sample data to simulate streamed JSON lines
// var DATA = []map[string]interface{}{
// 	{"data": map[string]interface{}{"id": "1", "message": "hello", "session_id": "sess-1", "search_prompt": "Go streaming"}},
// 	{"data": map[string]interface{}{"id": "2", "message": "world", "session_id": "sess-2", "search_prompt": "NDJSON"}},
// }

// Writer simulates an HTTP.ResponseWriter for testing
type Writer struct {
	bytes []byte
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.bytes = append(w.bytes, p...)
	return len(p), nil
}

func (w *Writer) Flush() {
	fmt.Print(string(w.bytes))
	w.bytes = nil
}

// Reader simulates an io.Reader that reads from a channel of byte slices
type Reader struct {
	c     <-chan []byte
	buf   []byte
	index int
}

func NewReader(ch <-chan []byte) *Reader {
	return &Reader{c: ch}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	// If we have buffered data, use it first
	for r.index >= len(r.buf) {
		b, ok := <-r.c
		if !ok {
			return 0, io.EOF
		}
		// Append newline to simulate NDJSON line
		r.buf = append(b, '\n')
		r.index = 0
	}

	// Copy as much as fits into p
	n = copy(p, r.buf[r.index:])
	r.index += n
	return n, nil
}

func handleStreamResponse(w io.Writer, body io.Reader, rp *Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, body)
		return
	}

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if rp.Transform == "json" {
			reader := NewJSONReader(line)
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
					continue
				}
				w.Write(outBytes)
				w.Write([]byte("\n"))
				flusher.Flush()
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

func main() {
	c := make(chan []byte, 10) // buffered to avoid goroutine leak
	w := &Writer{}
	r := NewReader(c)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(c)
		for _, item := range DATA {
			time.Sleep(100 * time.Millisecond)
			bytes, err := json.Marshal(item)
			if err != nil {
				log.Printf("Marshal error: %v", err)
				continue
			}
			c <- bytes
		}
	}()

	handleStreamResponse(w, r, &Response{
		Transform: "json",
		Stream:    true,
		Output: map[string]string{
			"id":            "[data][id]",
			"message":       "[data][message]",
			"session_id":    "[data][session_id]",
			"search_prompt": "[data][search_prompt]",
		},
	})

	wg.Wait()
}

var DATA = []map[string]any{
	{"success": true, "data": map[string]any{"id": "1", "message": "為", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "2", "message": "了", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "3", "message": "提", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "4", "message": "供", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "5", "message": "一", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "6", "message": "個", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "7", "message": "詳", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "8", "message": "細", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "9", "message": "且", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "10", "message": "長", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
	{"success": true, "data": map[string]any{"id": "11", "message": "篇", "session_id": "2025-12-09T06:44:11.449Z", "search_prompt": "\"請問您能否提供一些具體的內容或主題，讓我可以從中分析和整合資訊來創建一個詳細且長篇的回答？例如，如果有關於某個歷史事件、科學發現或文化主題的文件或文章片段，您可以分享給我。這樣我就能根據提供的信息來幫助您。\\n\\n如果沒有特定內容，請告訴我您想探討的主題或方向，以便我能更好地協助您。\""}},
}
