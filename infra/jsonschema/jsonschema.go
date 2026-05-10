// Package jsonschema is a tiny, deliberately-incomplete JSON Schema
// validator that covers the 90% of request-body validation needs:
//
//   - type: object | array | string | number | integer | boolean | null
//   - required: [list of keys] (object only)
//   - properties: { name: <subschema> } (object only)
//   - items: <subschema> (array only)
//   - enum: [allowed values]
//   - minimum / maximum (number/integer)
//   - minLength / maxLength (string)
//   - pattern (string, Go regexp)
//
// Intentionally avoids: $ref, oneOf/anyOf/allOf, format keyword, etc.
// Reach for github.com/santhosh-tekuri/jsonschema if you outgrow this.
//
// Validate returns *all* errors, not just the first one.
package jsonschema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Schema is the parsed in-memory representation. Build from raw JSON
// via Parse, then call Validate(value) where value is a Go any
// (the result of json.Unmarshal into interface{}).
type Schema struct {
	Type       string
	Required   []string
	Properties map[string]*Schema
	Items      *Schema
	Enum       []any
	Minimum    *float64
	Maximum    *float64
	MinLength  *int
	MaxLength  *int
	Pattern    *regexp.Regexp
	rawPattern string
}

// Parse builds a Schema from JSON bytes.
func Parse(b []byte) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// UnmarshalJSON applies the wire-format keys to our case-conventional
// fields. Done by hand so we can compile `pattern` once.
func (s *Schema) UnmarshalJSON(b []byte) error {
	var w struct {
		Type       string                     `json:"type"`
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
		Items      json.RawMessage            `json:"items"`
		Enum       []any                      `json:"enum"`
		Minimum    *float64                   `json:"minimum"`
		Maximum    *float64                   `json:"maximum"`
		MinLength  *int                       `json:"minLength"`
		MaxLength  *int                       `json:"maxLength"`
		Pattern    string                     `json:"pattern"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	s.Type = w.Type
	s.Required = w.Required
	s.Enum = w.Enum
	s.Minimum = w.Minimum
	s.Maximum = w.Maximum
	s.MinLength = w.MinLength
	s.MaxLength = w.MaxLength
	if w.Pattern != "" {
		re, err := regexp.Compile(w.Pattern)
		if err != nil {
			return fmt.Errorf("compile pattern %q: %w", w.Pattern, err)
		}
		s.Pattern = re
		s.rawPattern = w.Pattern
	}
	if len(w.Properties) > 0 {
		s.Properties = make(map[string]*Schema, len(w.Properties))
		for k, raw := range w.Properties {
			sub, err := Parse(raw)
			if err != nil {
				return fmt.Errorf("property %q: %w", k, err)
			}
			s.Properties[k] = sub
		}
	}
	if len(w.Items) > 0 {
		sub, err := Parse(w.Items)
		if err != nil {
			return fmt.Errorf("items: %w", err)
		}
		s.Items = sub
	}
	return nil
}

// Validate returns the slice of validation failures (empty when value
// conforms). path is the JSON pointer to the current node, used in
// error messages — pass "" at the entry point.
func (s *Schema) Validate(value any) []string {
	return s.validate("", value)
}

func (s *Schema) validate(path string, value any) []string {
	var errs []string
	at := func(field string) string {
		if path == "" {
			return field
		}
		return path + "." + field
	}

	if s.Type != "" && !typeMatches(s.Type, value) {
		errs = append(errs, fmt.Sprintf("%s: expected type %q, got %s", orRoot(path), s.Type, jsonType(value)))
		return errs
	}

	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if equalish(e, value) {
				ok = true
				break
			}
		}
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: value %v not in enum", orRoot(path), value))
		}
	}

	switch v := value.(type) {
	case map[string]any:
		for _, req := range s.Required {
			if _, ok := v[req]; !ok {
				errs = append(errs, fmt.Sprintf("%s: missing required property %q", orRoot(path), req))
			}
		}
		for name, sub := range s.Properties {
			child, ok := v[name]
			if !ok {
				continue
			}
			errs = append(errs, sub.validate(at(name), child)...)
		}
	case []any:
		if s.Items != nil {
			for i, item := range v {
				errs = append(errs, s.Items.validate(fmt.Sprintf("%s[%d]", orRoot(path), i), item)...)
			}
		}
	case string:
		if s.MinLength != nil && len(v) < *s.MinLength {
			errs = append(errs, fmt.Sprintf("%s: length %d < minLength %d", orRoot(path), len(v), *s.MinLength))
		}
		if s.MaxLength != nil && len(v) > *s.MaxLength {
			errs = append(errs, fmt.Sprintf("%s: length %d > maxLength %d", orRoot(path), len(v), *s.MaxLength))
		}
		if s.Pattern != nil && !s.Pattern.MatchString(v) {
			errs = append(errs, fmt.Sprintf("%s: %q does not match pattern %q", orRoot(path), v, s.rawPattern))
		}
	case float64:
		if s.Minimum != nil && v < *s.Minimum {
			errs = append(errs, fmt.Sprintf("%s: %v < minimum %v", orRoot(path), v, *s.Minimum))
		}
		if s.Maximum != nil && v > *s.Maximum {
			errs = append(errs, fmt.Sprintf("%s: %v > maximum %v", orRoot(path), v, *s.Maximum))
		}
	}

	return errs
}

func orRoot(p string) string {
	if p == "" {
		return "<root>"
	}
	return p
}

func typeMatches(want string, v any) bool {
	switch want {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		if !ok {
			return false
		}
		return f == float64(int64(f))
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	}
	return true
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func equalish(a, b any) bool {
	// Comparing decoded JSON values: primitives + slices via deep print.
	if a == b {
		return true
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// MustParse is a convenience for tests / static schemas.
func MustParse(b []byte) *Schema {
	s, err := Parse(b)
	if err != nil {
		panic(err)
	}
	return s
}

// Errors joins a slice of validation errors into a single readable
// string. Useful for "return one HTTP 400 with all problems listed".
func Errors(es []string) string { return strings.Join(es, "; ") }
