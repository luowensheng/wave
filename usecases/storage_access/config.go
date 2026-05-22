package storage_access

import (
	"github.com/luowensheng/wave/infra/inputs"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/luowensheng/wave/infra/render"
	"github.com/luowensheng/wave/io/http/contentloader"
)

// PipelineStep is one execution step in a multi-query pipeline.
//
// Inputs is a map[templateName → fromPath] that declares exactly which
// values this step's SQL template may reference and where to fetch them
// from the accumulated map. The accumulator is seeded with request inputs
// under the "inputs" namespace key (e.g. "inputs.user_id"), and each
// completed step's result is stored under its as: name.
//
//   fromPath "inputs.user_id" → accum["inputs"]["user_id"]  (request input)
//   fromPath "user.id"        → accum["user"]["id"]         (previous step result)
//   fromPath "orders.0.item"  → accum["orders"][0]["item"]  (slice index)
//
// All resolved values are passed through a strict-scope DataLoader so the
// SQL template sees only {{name}} → ? parameterised placeholders.
// Non-scalar values (maps, slices) are auto-JSON-encoded to a string.
type PipelineStep struct {
	Source  string            `yaml:"source"`
	Inputs  map[string]string `yaml:"inputs,omitempty"`
	Execute string            `yaml:"execute"`
	As      string            `yaml:"as"`
}

type Config struct {
	Source              string `yaml:"source"`
	Execute             string `yaml:"execute"`
	OutputTemplate      string `yaml:"output_template"`
	ResponseContentType string `yaml:"response_content_type"`
	ExpectedContentType string `yaml:"expected_content_type"`

	// IfEmptyStatus overrides the response status when the SQL/storage
	// execution returns no rows (or an empty result). Default 0 = use
	// 200 with the rendered template (back-compat). Set to 404 on GET
	// routes that should signal "not found" for missing keys
	// (kv-store, lookup-by-id endpoints, etc.).
	IfEmptyStatus int `yaml:"if_empty_status,omitempty"`

	// Steps enables pipeline mode: execute multiple queries in sequence,
	// feeding each result into the next as template data. When Steps is
	// set, Source and Execute are ignored. output_template is rendered
	// against the fully-accumulated result map.
	Steps []PipelineStep `yaml:"steps,omitempty"`
}

// StorageRef is the interface that storage backends must satisfy.
type StorageRef interface {
	Execute(command string, data *contentloader.DataLoader) (any, error)
}

// GetStorageFn is a function that retrieves a named storage backend.
// Inject this before using Config.CreateRoute.
var GetStorageFn func(name string) (StorageRef, bool)

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if c.OutputTemplate == "" && c.ResponseContentType != "$filetype" {
		return nil, fmt.Errorf("route output_template cannot be empty for path: %s", path)
	}

	if GetStorageFn == nil {
		return nil, fmt.Errorf("storage not configured")
	}

	// ── Pipeline mode ──────────────────────────────────────────────────────
	if len(c.Steps) > 0 {
		// Pre-resolve all step sources at route-creation time so we fail
		// fast at boot rather than at request time.
		type resolvedStep struct {
			storage StorageRef
			inputs  map[string]string // templateName → fromPath
			execute string
			as      string
		}
		steps := make([]resolvedStep, len(c.Steps))
		for i, s := range c.Steps {
			if s.Source == "" {
				return nil, fmt.Errorf("pipeline step %d: source is empty (path: %s)", i, path)
			}
			if s.Execute == "" {
				return nil, fmt.Errorf("pipeline step %d: execute is empty (path: %s)", i, path)
			}
			if s.As == "" {
				return nil, fmt.Errorf("pipeline step %d: as is empty (path: %s)", i, path)
			}
			st, found := GetStorageFn(s.Source)
			if !found {
				return nil, fmt.Errorf("pipeline step %d: undefined source %q (path: %s)", i, s.Source, path)
			}
			for inputName, fromPath := range s.Inputs {
				if fromPath == "" {
					return nil, fmt.Errorf("pipeline step %d: input %q: from-path is empty — write it explicitly (e.g. %q: \"inputs.%s\")",
						i, inputName, inputName, inputName)
				}
			}
			steps[i] = resolvedStep{storage: st, inputs: s.Inputs, execute: s.Execute, as: s.As}
		}

		return func(w http.ResponseWriter, r *http.Request) {
			// Seed accumulator with declared request inputs under "inputs" namespace.
			accum := make(map[string]any)
			if v := inputs.FromContext(r.Context()); len(v) > 0 {
				accum["inputs"] = v
			}

			// Execute each step with a strict-scope DataLoader built from
			// the step's declared inputs map.
			for _, step := range steps {
				// Resolve each declared input from the accumulated map.
				stepVals := make(map[string]any, len(step.inputs))
				for name, fromPath := range step.inputs {
					val, err := ResolvePath(accum, fromPath)
					if err != nil {
						log.Printf("pipeline step %q: input %q: resolve %q: %v", step.as, name, fromPath, err)
						http.Error(w, fmt.Sprintf("pipeline input error: %v", err), http.StatusInternalServerError)
						return
					}
					stepVals[name] = ToSQLParam(val)
				}

				dl := contentloader.NewDataLoaderFromContentLoader(r,
					contentloader.NewInputsLoader(stepVals))
				result, err := step.storage.Execute(step.execute, dl)
				if err != nil {
					log.Printf("pipeline step %q error: %v", step.as, err)
					http.Error(w, "internal server error", http.StatusInternalServerError)
					return
				}
				accum[step.as] = extractResultData(result)
			}

			if c.ResponseContentType != "" {
				w.Header().Set("Content-Type", c.ResponseContentType)
			}

			buffer, err := render.Render(c.OutputTemplate, accum)
			if err != nil {
				log.Printf("pipeline template render error: %v", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			w.Write(buffer.Bytes())
		}, nil
	}

	// ── Single-step mode ───────────────────────────────────────────────────
	if c.Source == "" {
		return nil, fmt.Errorf("route source cannot be empty for path: %s", path)
	}

	if c.Execute == "" {
		return nil, fmt.Errorf("route execute cannot be empty for path: %s", path)
	}

	storage, found := GetStorageFn(c.Source)
	if !found {
		return nil, fmt.Errorf("undefined source: '%s'", c.Source)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var dl *contentloader.DataLoader
		var err error
		var expectedContentType = c.ExpectedContentType

		// Strict-scope path: when the route declared `inputs:`, the
		// validated map is on the context. Build a DataLoader that
		// exposes ONLY those keys so the SQL template can't reference
		// anything outside the declared contract.
		if v := inputs.FromContext(r.Context()); len(v) > 0 {
			dl = contentloader.NewDataLoaderFromContentLoader(r,
				contentloader.NewInputsLoader(v))
		} else {
			switch method {
			case "POST", "PUT", "PATCH":
				if r.Body == nil {
					http.Error(w, "request body is required", http.StatusBadRequest)
					return
				}
				dl, err = contentloader.GetDataLoader(expectedContentType, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			default:
				dl, err = contentloader.GetDataLoader("application/x-www-form-urlencoded", r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
		}

		result, err := storage.Execute(c.Execute, dl)
		if err != nil {
			log.Printf("storage execution error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Empty-result short circuit: routes that opt in via
		// `if_empty_status:` get that status code returned with a tiny
		// JSON body, instead of rendering the template against nil.
		if c.IfEmptyStatus > 0 && isEmptyResult(result) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(c.IfEmptyStatus)
			_, _ = w.Write([]byte(`{"error":"not found"}` + "\n"))
			return
		}

		if c.ResponseContentType == "$filetype" {
			dataMap, ok := result.(map[string]any)
			if !ok {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			var f *contentloader.File

			for _, value := range dataMap {
				f, ok = value.(*contentloader.File)
				if ok {
					break
				}
			}

			if f == nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			rc, err := f.Reader.Open()
			if err != nil {
				http.Error(w, "Failed to open file", http.StatusInternalServerError)
				return
			}
			defer rc.Close()

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Filename))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", f.Size))
			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(f.Filename)))

			_, err = io.Copy(w, rc)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			return
		}

		if c.ResponseContentType != "" {
			w.Header().Set("Content-Type", c.ResponseContentType)
		}

		// Build template data:
		//   - No declared inputs → pass the raw result through unchanged
		//     so callers using `{{toJSON .}}` against a map[string]any
		//     storage backend (e.g. plugin storage adapters) get the old
		//     shape. Struct backends (sqlite ExecuteResult) keep their
		//     `.Data` / `.LastInsertID` fields exposed by reflection in
		//     the template engine itself.
		//   - Declared inputs present → merge result fields with input
		//     values so templates can reference both (`{{.Data}}` and
		//     `{{.slug}}` in the same template).
		var templateData any = result
		inputVals := inputs.FromContext(r.Context())
		if len(inputVals) > 0 {
			templateData = buildTemplateData(result, inputVals)
		}

		buffer, err := render.Render(c.OutputTemplate, templateData)
		if err != nil {
			log.Printf("template render error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Write(buffer.Bytes())
	}, nil
}

// ExtractResultData is the exported alias for extractResultData.
// Used by the scheduler's storage action and task sink.
func ExtractResultData(result any) any { return extractResultData(result) }

// extractResultData pulls the `.Data` field out of a backend result struct
// (e.g. *sqlite.ExecuteResult) via reflection. Falls back to the result
// itself for non-struct values. Used by the pipeline accumulator so that
// step.As holds the rows/map/scalar directly, not the wrapper struct.
func extractResultData(result any) any {
	if result == nil {
		return nil
	}
	rv := reflect.ValueOf(result)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		if f := rv.FieldByName("Data"); f.IsValid() && f.CanInterface() {
			return f.Interface()
		}
	}
	return result
}

// ResolvePath navigates a nested data structure using a dot-separated path.
// Each segment is either a map key (string) or a slice index (integer).
// The root is expected to be map[string]any but the function handles any
// value type along the path.
//
//	"user_id"       → root["user_id"]
//	"user.id"       → root["user"].(map)["id"]
//	"orders.0.item" → root["orders"].(slice)[0].(map)["item"]
func ResolvePath(root map[string]any, path string) (any, error) {
	parts := strings.Split(path, ".")
	var cur any = root
	for _, part := range parts {
		switch v := cur.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				return nil, fmt.Errorf("key %q not found", part)
			}
			cur = val
		case []map[string]any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("expected integer index for slice, got %q", part)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("index %d out of bounds (len=%d)", idx, len(v))
			}
			cur = v[idx]
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("expected integer index for slice, got %q", part)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("index %d out of bounds (len=%d)", idx, len(v))
			}
			cur = v[idx]
		default:
			return nil, fmt.Errorf("cannot navigate into %T at segment %q", cur, part)
		}
	}
	return cur, nil
}

// ToSQLParam converts a value to a type safe for use as a SQL parameter.
// Scalar types (string, int*, float*, bool, nil) are returned as-is.
// Non-scalar values (maps, slices) are JSON-encoded to a string so the
// SQLite driver can bind them without error.
func ToSQLParam(v any) any {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case string, []byte,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, bool:
		return v
	}
	// Non-scalar: JSON-encode.
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// buildTemplateData merges declared request inputs with result fields so
// output_template can reference both. Exported struct fields from the
// result (e.g. sqlite.ExecuteResult.Data, .LastInsertID) are added first;
// declared input values are then overlaid so user-named inputs are always
// reachable. The returned map[string]any causes render.Render to register
// every key as a bare template function AND as a dot-accessible field.
func buildTemplateData(result any, inputVals map[string]any) map[string]any {
	merged := make(map[string]any, len(inputVals)+8)

	if result != nil {
		rv := reflect.ValueOf(result)
		if rv.Kind() == reflect.Pointer && !rv.IsNil() {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			rt := rv.Type()
			for i := 0; i < rv.NumField(); i++ {
				f := rt.Field(i)
				if f.IsExported() {
					merged[f.Name] = rv.Field(i).Interface()
				}
			}
		} else {
			// Non-struct result ([]map, scalar, etc.): expose under "Data".
			merged["Data"] = result
		}
	}

	// Overlay declared inputs — they shadow same-named struct fields
	// (unlikely in practice, but inputs are the explicit user contract).
	for k, v := range inputVals {
		merged[k] = v
	}

	return merged
}

// isEmptyResult reports whether the storage Execute result is "no row".
// Handles the bare shapes (nil / empty slice / empty map) AND wrapped
// shapes that carry the rows under a `Data` field — covers both the
// SQLite ExecuteResult struct and the plugin-storage map wrapper.
func isEmptyResult(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case []map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		if data, ok := x["Data"]; ok {
			return isEmptyResult(data)
		}
		return len(x) == 0
	case string:
		return x == ""
	}
	// Reflect into struct/pointer types so backend-specific result
	// wrappers (sqlite.ExecuteResult etc.) work without explicit
	// imports.
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return true
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		// Look for an exported `Data` field; treat empty / nil as no row.
		if f := rv.FieldByName("Data"); f.IsValid() && f.CanInterface() {
			return isEmptyResult(f.Interface())
		}
	}
	return false
}
