// Package jsonpath is a small dot-or-bracket JSON path extractor used by
// the plugin and stream-publish routes for output mapping.
//
//	"response.id"          -> ["response", "id"]
//	"[response][user][0]"  -> ["response", "user", "0"]
//
// Index-into-array works because keys that parse as integers descend
// into JSON arrays.
package jsonpath

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Split converts a dotted or bracketed path into key segments.
func Split(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "[") {
		return splitBrackets(path)
	}
	return strings.Split(path, ".")
}

func splitBrackets(p string) []string {
	var (
		out  []string
		cur  strings.Builder
		open bool
	)
	for _, r := range p {
		switch r {
		case '[':
			open = true
		case ']':
			open = false
			out = append(out, cur.String())
			cur.Reset()
		default:
			if open {
				cur.WriteRune(r)
			}
		}
	}
	return out
}

// Extract walks parts through a JSON document and returns the value at
// that path as a generic Go value. Returns false if any segment is missing.
func Extract(raw json.RawMessage, parts []string) (any, bool) {
	if len(parts) == 0 {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, false
		}
		return v, true
	}

	head := parts[0]

	// Try as object first.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		next, ok := obj[head]
		if !ok {
			return nil, false
		}
		return Extract(next, parts[1:])
	}

	// Then as array (head must be an integer index).
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		idx, err := strconv.Atoi(head)
		if err != nil || idx < 0 || idx >= len(arr) {
			return nil, false
		}
		return Extract(arr[idx], parts[1:])
	}

	return nil, false
}

// Apply runs Extract for every output mapping over the same source
// document, then merges in static metadata (which always wins on key
// collisions, matching the spec's "constant metadata" intent).
//
// Returns the filtered map ready to be json.Marshal'd.
func Apply(src json.RawMessage, output map[string]string, static map[string]string) map[string]any {
	result := make(map[string]any, len(output)+len(static))
	for outKey, srcPath := range output {
		v, ok := Extract(src, Split(srcPath))
		if !ok {
			continue
		}
		result[outKey] = v
	}
	for k, v := range static {
		result[k] = v
	}
	return result
}

// MustEncode is a tiny helper for tests / error paths.
func MustEncode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("jsonpath: marshal: %w", err))
	}
	return b
}
