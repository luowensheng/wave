package task

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/luowensheng/wave/infra/connections"
	"github.com/luowensheng/wave/infra/inputs"
	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/io/http/contentloader"
	storageaccess "github.com/luowensheng/wave/usecases/storage_access"
)

// GetStorageFn retrieves a named storage backend. Set at boot by the orchestrator.
var GetStorageFn func(name string) (storageaccess.StorageRef, bool)

// StoreConfig defines optional persistence for each emitted event.
type StoreConfig struct {
	Source string `yaml:"source"`
	// Inputs maps SQL template name → dot-path into the emitted event accumulator.
	// The payload is stored under "event", so paths take the form "event.<field>"
	// (e.g. "event.content", "event.status"). Empty strings are rejected at boot.
	Inputs  map[string]string `yaml:"inputs,omitempty"`
	Execute string            `yaml:"execute"`
}

// Config is the YAML shape for `type: task` routes.
type Config struct {
	Plugin     string       `yaml:"plugin"`
	TriggerKey string       `yaml:"trigger_key"`
	Streaming  bool         `yaml:"streaming"`   // read plugin response body as ndjson
	Connection string       `yaml:"connection"`  // SSE broker name (required)
	EventType  string       `yaml:"event_type"`  // SSE event type label
	Store      *StoreConfig `yaml:"store,omitempty"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if c.Plugin == "" {
		return nil, fmt.Errorf("task route %q: plugin is required", path)
	}
	if c.Connection == "" {
		return nil, fmt.Errorf("task route %q: connection is required", path)
	}

	// Validate store config at boot
	if c.Store != nil {
		if c.Store.Source == "" {
			return nil, fmt.Errorf("task route %q: store.source is required", path)
		}
		if c.Store.Execute == "" {
			return nil, fmt.Errorf("task route %q: store.execute is required", path)
		}
		if GetStorageFn == nil {
			return nil, fmt.Errorf("task route %q: store configured but storage not wired", path)
		}
		if _, ok := GetStorageFn(c.Store.Source); !ok {
			return nil, fmt.Errorf("task route %q: store source %q not found", path, c.Store.Source)
		}
		for inputName, fromPath := range c.Store.Inputs {
			if fromPath == "" {
				return nil, fmt.Errorf("task route %q: store input %q: from-path is empty — write it explicitly",
					path, inputName)
			}
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Read body before goroutine launch (r.Body is consumed once).
		var bodyBytes []byte
		if r.Body != nil {
			buf := new(bytes.Buffer)
			buf.ReadFrom(r.Body)
			bodyBytes = buf.Bytes()
		}

		// Capture declared inputs from context.
		inputVals := inputs.FromContext(r.Context())

		taskID := newTaskID()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"task_id":%q}`+"\n", taskID)

		go c.run(taskID, bodyBytes, inputVals)
	}, nil
}

func (c *Config) run(taskID string, bodyBytes []byte, inputVals map[string]any) {
	ctx := context.Background()

	reg := plugins.Default()
	if reg == nil {
		log.Printf("task %s: plugin registry not initialized", taskID)
		return
	}
	client, ok := reg.Get(c.Plugin)
	if !ok {
		log.Printf("task %s: plugin %q not found", taskID, c.Plugin)
		return
	}

	// Build metadata from declared input values.
	meta := map[string]string{"task_id": taskID, "source": "task"}

	resp, err := client.Call(ctx, &plugins.Request{
		TriggerKey: c.TriggerKey,
		Metadata:   meta,
		Body:       bodyBytes,
	})
	if err != nil {
		log.Printf("task %s: plugin call error: %v", taskID, err)
		return
	}

	if c.Streaming {
		// Read ndjson lines from response body.
		scanner := bufio.NewScanner(bytes.NewReader(resp.Body))
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			c.emit(taskID, line, inputVals)
		}
	} else {
		if len(resp.Body) > 0 {
			c.emit(taskID, resp.Body, inputVals)
		}
	}
}

func (c *Config) emit(taskID string, payload []byte, inputVals map[string]any) {
	// Publish to SSE broker.
	reg := connections.Default()
	if reg != nil {
		if broker, ok := reg.Get(c.Connection); ok {
			broker.Publish(buildSSEEvent(c.EventType, payload))
		} else {
			log.Printf("task %s: connection %q not found", taskID, c.Connection)
		}
	}

	// Optionally write to storage.
	if c.Store == nil || GetStorageFn == nil {
		return
	}
	storage, ok := GetStorageFn(c.Store.Source)
	if !ok {
		log.Printf("task %s: store source %q not found", taskID, c.Store.Source)
		return
	}

	// Decode payload into map and store under "event" namespace.
	// Dot-paths in store.inputs must reference event.* (e.g. "event.content").
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		// Not a JSON object — wrap raw string under "data".
		payloadMap = map[string]any{"data": string(payload)}
	}
	accum := map[string]any{"event": payloadMap}

	// Resolve inputs from the accumulator using dot-paths.
	stepVals := make(map[string]any, len(c.Store.Inputs))
	for name, fromPath := range c.Store.Inputs {
		val, err := storageaccess.ResolvePath(accum, fromPath)
		if err != nil {
			log.Printf("task %s: store input %q: resolve %q: %v", taskID, name, fromPath, err)
			return
		}
		stepVals[name] = storageaccess.ToSQLParam(val)
	}

	// Create a minimal *http.Request for NewDataLoaderFromContentLoader
	// (used only to satisfy the signature; the DataLoader reads from InputsLoader).
	fakeReq, _ := http.NewRequest("GET", "/", nil)
	dl := contentloader.NewDataLoaderFromContentLoader(fakeReq,
		contentloader.NewInputsLoader(stepVals))

	if _, err := storage.Execute(c.Store.Execute, dl); err != nil {
		log.Printf("task %s: store execute error: %v", taskID, err)
	}
}

// buildSSEEvent formats a server-sent event.
func buildSSEEvent(eventType string, payload []byte) []byte {
	var buf bytes.Buffer
	if eventType != "" {
		fmt.Fprintf(&buf, "event: %s\n", eventType)
	}
	fmt.Fprintf(&buf, "data: %s\n\n", payload)
	return buf.Bytes()
}

func newTaskID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID.
		return fmt.Sprintf("task-%d", len(b))
	}
	return hex.EncodeToString(b)
}
