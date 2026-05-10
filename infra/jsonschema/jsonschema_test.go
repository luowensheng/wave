package jsonschema

import (
	"encoding/json"
	"testing"
)

func parseValue(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestObjectRequired(t *testing.T) {
	s := MustParse([]byte(`{"type":"object","required":["name","age"]}`))
	if errs := s.Validate(parseValue(t, `{"name":"a","age":30}`)); len(errs) != 0 {
		t.Errorf("unexpected errs: %v", errs)
	}
	errs := s.Validate(parseValue(t, `{"name":"a"}`))
	if len(errs) != 1 {
		t.Fatalf("got %v", errs)
	}
}

func TestPropertiesRecursive(t *testing.T) {
	s := MustParse([]byte(`{
		"type":"object",
		"properties": {
			"user": { "type":"object", "required":["email"],
				"properties": { "email": {"type":"string","pattern":"^[^@]+@[^@]+$"} } }
		}
	}`))
	if errs := s.Validate(parseValue(t, `{"user":{"email":"a@b"}}`)); len(errs) != 0 {
		t.Errorf("good: %v", errs)
	}
	errs := s.Validate(parseValue(t, `{"user":{"email":"nope"}}`))
	if len(errs) != 1 {
		t.Errorf("bad pattern should fail: %v", errs)
	}
	errs = s.Validate(parseValue(t, `{"user":{}}`))
	if len(errs) != 1 {
		t.Errorf("missing required should fail: %v", errs)
	}
}

func TestArrayItems(t *testing.T) {
	s := MustParse([]byte(`{"type":"array","items":{"type":"integer","minimum":0}}`))
	if errs := s.Validate(parseValue(t, `[1,2,3]`)); len(errs) != 0 {
		t.Errorf("good: %v", errs)
	}
	errs := s.Validate(parseValue(t, `[1,-1,2]`))
	if len(errs) != 1 {
		t.Errorf("got %v", errs)
	}
}

func TestEnumAndNumberBounds(t *testing.T) {
	s := MustParse([]byte(`{"type":"string","enum":["a","b"]}`))
	if errs := s.Validate(parseValue(t, `"a"`)); len(errs) != 0 {
		t.Errorf("good: %v", errs)
	}
	if errs := s.Validate(parseValue(t, `"c"`)); len(errs) != 1 {
		t.Errorf("bad enum: %v", errs)
	}
	num := MustParse([]byte(`{"type":"number","minimum":1,"maximum":10}`))
	if errs := num.Validate(parseValue(t, `5`)); len(errs) != 0 {
		t.Errorf("in-range: %v", errs)
	}
	if errs := num.Validate(parseValue(t, `42`)); len(errs) != 1 {
		t.Errorf("over: %v", errs)
	}
}

func TestStringLength(t *testing.T) {
	s := MustParse([]byte(`{"type":"string","minLength":2,"maxLength":4}`))
	if errs := s.Validate(parseValue(t, `"abc"`)); len(errs) != 0 {
		t.Errorf("good: %v", errs)
	}
	if errs := s.Validate(parseValue(t, `"a"`)); len(errs) != 1 {
		t.Errorf("short: %v", errs)
	}
	if errs := s.Validate(parseValue(t, `"abcdef"`)); len(errs) != 1 {
		t.Errorf("long: %v", errs)
	}
}

func TestTypeMismatch(t *testing.T) {
	s := MustParse([]byte(`{"type":"integer"}`))
	if errs := s.Validate(parseValue(t, `1`)); len(errs) != 0 {
		t.Errorf("integer ok: %v", errs)
	}
	if errs := s.Validate(parseValue(t, `1.5`)); len(errs) != 1 {
		t.Errorf("float should fail integer: %v", errs)
	}
}
