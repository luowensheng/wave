package schedule

import (
	"strings"
	"testing"
)

func TestValidateAction_ValidAPINoSinks(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com"}
	if err := ValidateAction("job1", action, nil); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateAction_ValidAPIWithSinksAndOutput(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "result"}
	sinks := []*Sink{
		{Type: "publish", Connection: "events"},
	}
	if err := ValidateAction("job1", action, sinks); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateAction_MissingOutputWithSinks(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com"}
	sinks := []*Sink{
		{Type: "publish", Connection: "events"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for missing output, got nil")
	}
	if !strings.Contains(err.Error(), "output") {
		t.Errorf("expected error to mention 'output', got: %v", err)
	}
}

func TestValidateAction_UnknownActionType(t *testing.T) {
	action := &Action{Type: "grpc"}
	err := ValidateAction("job1", action, nil)
	if err == nil {
		t.Fatal("expected error for unknown action type, got nil")
	}
}

func TestValidateAction_StorageSinkMissingSource(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "storage", Execute: "SELECT 1"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for missing storage source, got nil")
	}
}

func TestValidateAction_StorageSinkMissingExecute(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "storage", Source: "db"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for missing storage execute, got nil")
	}
}

func TestValidateAction_PublishSinkMissingConnection(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "publish"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for missing publish connection, got nil")
	}
}

func TestValidateAction_PluginSinkMissingPlugin(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "plugin"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for missing plugin name, got nil")
	}
}

func TestValidateAction_SinkWithEmptyFromPath(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{
			Type:    "storage",
			Source:  "db",
			Execute: "INSERT INTO t (v) VALUES ({{v}})",
			Inputs:  map[string]string{"v": ""},
		},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for empty from-path, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected error to contain 'from-path is empty', got: %v", err)
	}
}

func TestValidateAction_UnknownSinkType(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "webhook"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error for unknown sink type, got nil")
	}
}

func TestValidateAction_APISinkMissingRefAndURL(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "api"},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ref or url is required") {
		t.Errorf("expected 'ref or url is required' in error, got: %v", err)
	}
}

func TestValidateAction_APISinkEmptyVar(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "api", URL: "http://x", Vars: map[string]string{"x": ""}},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected 'from-path is empty' in error, got: %v", err)
	}
}

func TestValidateAction_ForEachMissingIn(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{
			Type: "for_each",
			As:   "x",
			Do:   []*Sink{{Type: "publish", Connection: "c"}},
		},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "in is required") {
		t.Errorf("expected 'in is required' in error, got: %v", err)
	}
}

func TestValidateAction_ForEachMissingAs(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "x",
			Do:   []*Sink{{Type: "publish", Connection: "c"}},
		},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "as is required") {
		t.Errorf("expected 'as is required' in error, got: %v", err)
	}
}

func TestValidateAction_ForEachEmptyDo(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{Type: "for_each", In: "x", As: "y", Do: nil},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "do must contain at least one") {
		t.Errorf("expected 'do must contain at least one' in error, got: %v", err)
	}
}

func TestValidateAction_ForEachNestedValidation(t *testing.T) {
	action := &Action{Type: "api", URL: "http://example.com", Output: "res"}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "items",
			As:   "item",
			Do: []*Sink{
				{
					Type:    "storage",
					Source:  "db",
					Execute: "INSERT INTO t (v) VALUES ({{v}})",
					Inputs:  map[string]string{"v": ""},
				},
			},
		},
	}
	err := ValidateAction("job1", action, sinks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected 'from-path is empty' in error, got: %v", err)
	}
}
