package jsonpath

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"response.id", []string{"response", "id"}},
		{"a.b.c", []string{"a", "b", "c"}},
		{"[response][id]", []string{"response", "id"}},
		{"[response][user][0]", []string{"response", "user", "0"}},
		{"", nil},
	}
	for _, c := range cases {
		got := Split(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Split(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestApplyFiltersAndMerges(t *testing.T) {
	src := json.RawMessage(`{"id":"pi_1","amount":2000,"secret":"sk_x","items":[{"name":"a"},{"name":"b"}]}`)
	out := Apply(src,
		map[string]string{
			"payment_id": "id",
			"amount":     "amount",
			"first_item": "items.0.name",
		},
		map[string]string{
			"source": "stripe",
		},
	)
	if out["payment_id"] != "pi_1" {
		t.Errorf("payment_id = %v", out["payment_id"])
	}
	if v, ok := out["amount"].(float64); !ok || v != 2000 {
		t.Errorf("amount = %v", out["amount"])
	}
	if out["first_item"] != "a" {
		t.Errorf("first_item = %v", out["first_item"])
	}
	if out["source"] != "stripe" {
		t.Errorf("source = %v", out["source"])
	}
	if _, leaked := out["secret"]; leaked {
		t.Errorf("secret leaked into output")
	}
}

func TestApplyMissingPathSkips(t *testing.T) {
	src := json.RawMessage(`{"a":1}`)
	out := Apply(src, map[string]string{
		"present": "a",
		"missing": "b.c.d",
	}, nil)
	if _, ok := out["missing"]; ok {
		t.Errorf("missing path should be skipped, got %v", out)
	}
	if _, ok := out["present"]; !ok {
		t.Errorf("present should be set")
	}
}
