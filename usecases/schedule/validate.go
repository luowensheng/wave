package schedule

import "fmt"

// ValidateAction checks an action+sinks config at boot time.
// contextName is the job name or route path for error messages.
func ValidateAction(contextName string, action *Action, sinks []*Sink) error {
	if action == nil {
		return fmt.Errorf("%s: action is required", contextName)
	}
	validTypes := map[string]bool{"api": true, "plugin": true, "storage": true}
	if !validTypes[action.Type] {
		return fmt.Errorf("%s: action.type must be api, plugin, or storage (got %q)", contextName, action.Type)
	}
	if len(sinks) > 0 && action.Output == "" {
		return fmt.Errorf("%s: action.output is required when then: is non-empty", contextName)
	}
	for i, sink := range sinks {
		if err := validateSink(contextName, i, sink); err != nil {
			return err
		}
	}
	return nil
}

// validateSink validates a sink at top-level position i within contextName.
// Errors are prefixed "<contextName>: then[<i>]: ...".
func validateSink(contextName string, i int, sink *Sink) error {
	prefix := fmt.Sprintf("%s: then[%d]", contextName, i)
	return validateSinkAt(prefix, sink)
}

// validateSinkAt validates a sink whose error-message prefix has already been built.
// Used by validateSink and recursively by for_each.
func validateSinkAt(prefix string, sink *Sink) error {
	for inputName, fromPath := range sink.Inputs {
		if fromPath == "" {
			return fmt.Errorf("%s: input %q: from-path is empty — write it explicitly (e.g. %q: %q)",
				prefix, inputName, inputName, inputName+".field")
		}
	}
	switch sink.Type {
	case "storage":
		if sink.Source == "" {
			return fmt.Errorf("%s: storage sink: source is required", prefix)
		}
		if sink.Execute == "" {
			return fmt.Errorf("%s: storage sink: execute is required", prefix)
		}
	case "publish":
		if sink.Connection == "" {
			return fmt.Errorf("%s: publish sink: connection is required", prefix)
		}
	case "plugin":
		if sink.Plugin == "" {
			return fmt.Errorf("%s: plugin sink: plugin is required", prefix)
		}
	case "api":
		if sink.Ref == "" && sink.URL == "" {
			return fmt.Errorf("%s: api sink: ref or url is required", prefix)
		}
		for varName, fromPath := range sink.Vars {
			if fromPath == "" {
				return fmt.Errorf("%s: api sink var %q: from-path is empty — write it explicitly", prefix, varName)
			}
		}
	case "for_each":
		if sink.In == "" {
			return fmt.Errorf("%s: for_each sink: in is required", prefix)
		}
		if sink.As == "" {
			return fmt.Errorf("%s: for_each sink: as is required", prefix)
		}
		if len(sink.Do) == 0 {
			return fmt.Errorf("%s: for_each sink: do must contain at least one nested sink", prefix)
		}
		for j, nested := range sink.Do {
			nestedPrefix := fmt.Sprintf("%s.do[%d]", prefix, j)
			if err := validateSinkAt(nestedPrefix, nested); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: unknown sink type %q", prefix, sink.Type)
	}
	return nil
}
