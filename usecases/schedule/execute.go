package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"wave/infra/connections"
	"wave/infra/httpclient"
	"wave/infra/plugins"
	"wave/io/http/contentloader"
	storageaccess "wave/usecases/storage_access"
)

// Injected dependencies — set at boot by InitDependencies.
var GetStorageFn func(name string) (storageaccess.StorageRef, bool)
var GetConnectionFn func(name string) (*connections.Broker, bool)
var GetPluginFn func(name string) (plugins.Client, bool)

// resolveVars resolves each var's dot-path against the accumulator.
func resolveVars(accum map[string]any, varDecls map[string]string) (map[string]any, error) {
	vars := make(map[string]any, len(varDecls))
	for varName, fromPath := range varDecls {
		val, err := storageaccess.ResolvePath(accum, fromPath)
		if err != nil {
			return nil, fmt.Errorf("var %q: resolve %q: %w", varName, fromPath, err)
		}
		vars[varName] = val
	}
	return vars, nil
}

// executeAPICall performs the outbound HTTP call described by ref+vars or inline fields.
// Used by both type:api actions and type:api sinks.
func executeAPICall(ctx context.Context, ref string, url string, method string, headers map[string]string, body string, vars map[string]any) (map[string]any, error) {
	var def *httpclient.RequestDef
	if ref != "" {
		reg := httpclient.Default()
		if reg == nil {
			return nil, fmt.Errorf("api: httpclient registry not initialized")
		}
		var ok bool
		def, ok = reg.Get(ref)
		if !ok {
			return nil, fmt.Errorf("api: request ref %q not found", ref)
		}
	} else {
		def = &httpclient.RequestDef{URL: url, Method: method, Headers: headers, Body: body}
	}
	return def.Execute(ctx, vars)
}

// ExecuteAction runs the configured action against the accumulator.
// For route handlers, accum["inputs"] holds all declared route inputs.
// For scheduled jobs, accum starts empty.
// Returns the result map, which the caller stores under action.Output.
func ExecuteAction(ctx context.Context, action *Action, accum map[string]any) (map[string]any, error) {
	vars, err := resolveVars(accum, action.Vars)
	if err != nil {
		return nil, fmt.Errorf("action %w", err)
	}

	switch action.Type {
	case "api":
		return executeAPICall(ctx, action.Ref, action.URL, action.Method, action.Headers, action.Body, vars)

	case "plugin":
		if GetPluginFn == nil {
			return nil, fmt.Errorf("plugin action: plugin registry not initialized")
		}
		client, ok := GetPluginFn(action.Plugin)
		if !ok {
			return nil, fmt.Errorf("plugin action: %q not found", action.Plugin)
		}
		var bodyBytes []byte
		if action.Body != "" {
			bodyBytes = []byte(action.Body)
		} else {
			bodyBytes, _ = json.Marshal(vars)
		}
		resp, err := client.Call(ctx, &plugins.Request{
			TriggerKey: action.TriggerKey,
			Body:       bodyBytes,
		})
		if err != nil {
			return nil, fmt.Errorf("plugin action: %w", err)
		}
		// Plugin action result mirrors the api action shape so sinks
		// can use the same dot-paths regardless of action type:
		//   text   string         — raw plugin response body as a string
		//   json   map[string]any — parsed body iff valid JSON object
		//   status int            — plugin response status
		result := map[string]any{
			"text":   string(resp.Body),
			"status": resp.Status,
		}
		var parsed map[string]any
		if err := json.Unmarshal(resp.Body, &parsed); err == nil {
			result["json"] = parsed
		}
		return result, nil

	case "storage":
		if GetStorageFn == nil {
			return nil, fmt.Errorf("storage action: storage not configured")
		}
		st, ok := GetStorageFn(action.Source)
		if !ok {
			return nil, fmt.Errorf("storage action: source %q not found", action.Source)
		}
		fakeReq, _ := http.NewRequest("GET", "/", nil)
		dl := contentloader.NewDataLoaderFromContentLoader(fakeReq, contentloader.NewInputsLoader(nil))
		result, err := st.Execute(action.Execute, dl)
		if err != nil {
			return nil, fmt.Errorf("storage action: %w", err)
		}
		return map[string]any{"data": storageaccess.ExtractResultData(result)}, nil

	default:
		return nil, fmt.Errorf("unknown action type %q", action.Type)
	}
}

// ApplySinks runs each sink against the accumulator.
func ApplySinks(ctx context.Context, sinks []*Sink, accum map[string]any) error {
	for _, sink := range sinks {
		if err := applySink(ctx, sink, accum); err != nil {
			// Per-sink failure policy. "continue"/"skip" turns a hard error
			// into a logged skip so one bad item (e.g. an Atom feed with no
			// rss.channel.item) cannot abort the whole tick / drain loop.
			if sink.OnError == "continue" || sink.OnError == "skip" {
				log.Printf("schedule: sink[type=%s] error (on_error=%s; skipping): %v",
					sink.Type, sink.OnError, err)
				continue
			}
			return fmt.Errorf("sink[type=%s]: %w", sink.Type, err)
		}
	}
	return nil
}

func applySink(ctx context.Context, sink *Sink, accum map[string]any) error {
	switch sink.Type {
	case "storage":
		if GetStorageFn == nil {
			return fmt.Errorf("storage not configured")
		}
		st, ok := GetStorageFn(sink.Source)
		if !ok {
			return fmt.Errorf("source %q not found", sink.Source)
		}
		stepVals := make(map[string]any, len(sink.Inputs))
		for name, fromPath := range sink.Inputs {
			val, err := storageaccess.ResolvePath(accum, fromPath)
			if err != nil {
				return fmt.Errorf("input %q: resolve %q: %w", name, fromPath, err)
			}
			stepVals[name] = storageaccess.ToSQLParam(val)
		}
		fakeReq, _ := http.NewRequest("GET", "/", nil)
		dl := contentloader.NewDataLoaderFromContentLoader(fakeReq, contentloader.NewInputsLoader(stepVals))
		if _, err := st.Execute(sink.Execute, dl); err != nil {
			return fmt.Errorf("storage execute: %w", err)
		}
		return nil

	case "publish":
		if GetConnectionFn == nil {
			return fmt.Errorf("connection registry not initialized")
		}
		broker, ok := GetConnectionFn(sink.Connection)
		if !ok {
			return fmt.Errorf("connection %q not found", sink.Connection)
		}
		payload, _ := json.Marshal(accum)
		var buf []byte
		if sink.EventType != "" {
			buf = append(buf, []byte("event: "+sink.EventType+"\n")...)
		}
		buf = append(buf, []byte("data: "+string(payload)+"\n\n")...)
		broker.Publish(buf)
		return nil

	case "plugin":
		if GetPluginFn == nil {
			return fmt.Errorf("plugin registry not initialized")
		}
		client, ok := GetPluginFn(sink.Plugin)
		if !ok {
			return fmt.Errorf("plugin %q not found", sink.Plugin)
		}
		b, _ := json.Marshal(accum)
		_, err := client.Call(ctx, &plugins.Request{
			TriggerKey: sink.TriggerKey,
			Body:       b,
		})
		return err

	case "api":
		vars, err := resolveVars(accum, sink.Vars)
		if err != nil {
			return err
		}
		result, err := executeAPICall(ctx, sink.Ref, sink.URL, sink.Method, sink.Headers, sink.Body, vars)
		if err != nil {
			return err
		}
		if sink.Output != "" {
			accum[sink.Output] = result
		}
		return nil

	case "for_each":
		raw, err := storageaccess.ResolvePath(accum, sink.In)
		if err != nil {
			return fmt.Errorf("for_each: resolve %q: %w", sink.In, err)
		}
		items, err := toIterable(raw)
		if err != nil {
			return fmt.Errorf("for_each: %w", err)
		}
		for idx, item := range items {
			iterAccum := cloneAccum(accum)
			iterAccum[sink.As] = item
			if err := ApplySinks(ctx, sink.Do, iterAccum); err != nil {
				return fmt.Errorf("for_each[%d]: %w", idx, err)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown sink type %q", sink.Type)
	}
}

// toIterable normalizes a value into []any for for_each iteration.
// Accepts []any and []map[string]any. A single map[string]any is treated
// as a one-element list — this is the standard generic XML→map
// single-vs-many edge (an element occurring once is not wrapped in a
// slice, e.g. an RSS feed with exactly one <item>), so iterating it is
// well-defined rather than an error. Other scalar types are rejected.
func toIterable(v any) ([]any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return x, nil
	case []map[string]any:
		out := make([]any, len(x))
		for i, m := range x {
			out[i] = m
		}
		return out, nil
	case map[string]any:
		return []any{x}, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", v)
	}
}

// cloneAccum makes a shallow copy of the top-level map so per-iteration
// writes (e.g. sink.Output) don't bleed into sibling iterations.
func cloneAccum(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
