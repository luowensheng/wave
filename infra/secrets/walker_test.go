package secrets

import (
	"testing"
)

type leaf struct {
	Name   string
	Secret string
}

type nest struct {
	Title   string
	Inner   leaf
	InnerP  *leaf
	List    []string
	Items   []leaf
	Map     map[string]string
	StrMap  map[string]leaf
	private string //nolint:unused
}

func setupTestPluginResolver(t *testing.T, fn PluginResolverFn) {
	t.Helper()
	SetPluginResolver(fn)
	t.Cleanup(func() { SetPluginResolver(nil) })
}

func TestExpandStruct_StringField(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("RESOLVED:" + name + ":" + uri), nil
	})
	v := &leaf{Name: "x", Secret: "${PLUGIN:vault:k1}"}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.Secret != "RESOLVED:vault:k1" {
		t.Errorf("got %q", v.Secret)
	}
	if v.Name != "x" {
		t.Errorf("name mutated: %q", v.Name)
	}
}

func TestExpandStruct_NestedStruct(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("ok-" + uri), nil
	})
	v := &nest{
		Title:  "${PLUGIN:p:t}",
		Inner:  leaf{Secret: "${PLUGIN:p:i}"},
		InnerP: &leaf{Secret: "${PLUGIN:p:ip}"},
	}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.Title != "ok-t" {
		t.Errorf("title %q", v.Title)
	}
	if v.Inner.Secret != "ok-i" {
		t.Errorf("inner %q", v.Inner.Secret)
	}
	if v.InnerP.Secret != "ok-ip" {
		t.Errorf("innerp %q", v.InnerP.Secret)
	}
}

func TestExpandStruct_StringSlice(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("R:" + uri), nil
	})
	v := &nest{List: []string{"plain", "${PLUGIN:p:a}", "${PLUGIN:p:b}"}}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.List[0] != "plain" || v.List[1] != "R:a" || v.List[2] != "R:b" {
		t.Errorf("got %v", v.List)
	}
}

func TestExpandStruct_StructSlice(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("ok-" + uri), nil
	})
	v := &nest{Items: []leaf{{Secret: "${PLUGIN:p:1}"}, {Secret: "${PLUGIN:p:2}"}}}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.Items[0].Secret != "ok-1" || v.Items[1].Secret != "ok-2" {
		t.Errorf("got %+v", v.Items)
	}
}

func TestExpandStruct_StringMap(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("R:" + uri), nil
	})
	v := &nest{Map: map[string]string{
		"${PLUGIN:p:keyish}": "${PLUGIN:p:val}",
		"plain":              "ok",
	}}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	// Key must be untouched.
	if _, ok := v.Map["${PLUGIN:p:keyish}"]; !ok {
		t.Errorf("key was mutated: %v", v.Map)
	}
	if v.Map["${PLUGIN:p:keyish}"] != "R:val" {
		t.Errorf("val %q", v.Map["${PLUGIN:p:keyish}"])
	}
}

func TestExpandStruct_MapOfStruct(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("R:" + uri), nil
	})
	v := &nest{StrMap: map[string]leaf{"a": {Secret: "${PLUGIN:p:a}"}}}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.StrMap["a"].Secret != "R:a" {
		t.Errorf("got %+v", v.StrMap)
	}
}

func TestExpandStruct_NilPointer(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("never"), nil
	})
	var v *leaf
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
}

func TestExpandStruct_NoMarkersNoChange(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		t.Fatalf("resolver must not be called")
		return nil, nil
	})
	v := &leaf{Name: "n", Secret: "no markers here"}
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
}

func TestFindMarkers(t *testing.T) {
	v := &nest{
		Title: "${PLUGIN:foo:1}",
		Inner: leaf{Secret: "${PLUGIN:bar:2}"},
		List:  []string{"${ENV:OTHER}"},
		Map:   map[string]string{"x": "${PLUGIN:baz:3}"},
	}
	got := FindMarkers(v, "PLUGIN")
	if len(got) != 3 {
		t.Errorf("expected 3 markers, got %d: %v", len(got), got)
	}
}

type cyc struct {
	Name string
	Self *cyc
}

func TestExpandStruct_CycleSafety(t *testing.T) {
	setupTestPluginResolver(t, func(name, uri string) ([]byte, error) {
		return []byte("ok"), nil
	})
	v := &cyc{Name: "${PLUGIN:p:n}"}
	v.Self = v
	if err := ExpandStruct(v); err != nil {
		t.Fatal(err)
	}
	if v.Name != "ok" {
		t.Errorf("got %q", v.Name)
	}
}

func TestSetPluginResolver_NilRemovesPrefix(t *testing.T) {
	SetPluginResolver(func(name, uri string) ([]byte, error) {
		return []byte("X"), nil
	})
	got, err := Expand("${PLUGIN:p:k}")
	if err != nil || got != "X" {
		t.Fatalf("got %q err %v", got, err)
	}
	SetPluginResolver(nil)
	got, err = Expand("${PLUGIN:p:k}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "${PLUGIN:p:k}" {
		t.Errorf("expected marker preserved, got %q", got)
	}
}
