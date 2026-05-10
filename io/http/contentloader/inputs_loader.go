package contentloader

import (
	"fmt"
	"net/http"
)

// NewDataLoaderFromContentLoader wraps an arbitrary ContentLoader in
// a DataLoader so storage_access can use it the same way as a body /
// form loader. Useful when the caller has already produced a
// validated values map (e.g. from infra/inputs).
func NewDataLoaderFromContentLoader(r *http.Request, l ContentLoader) *DataLoader {
	return &DataLoader{r: r, loader: l}
}

// InputsContentLoader exposes only the declared, validated inputs map
// (typically from infra/inputs.FromContext). Used to enforce
// strict-scope template substitution in storage_access — the SQL
// template can reference only what's been declared, no surprises.
type InputsContentLoader struct {
	values map[string]any
}

// NewInputsLoader wraps a values map.
func NewInputsLoader(values map[string]any) *InputsContentLoader {
	if values == nil {
		values = map[string]any{}
	}
	return &InputsContentLoader{values: values}
}

func (l *InputsContentLoader) GetValue(name string) (any, error) {
	v, ok := l.values[name]
	if !ok {
		return nil, fmt.Errorf("undeclared input %q", name)
	}
	return v, nil
}

func (l *InputsContentLoader) GetFile(name string) (*File, error) {
	return nil, fmt.Errorf("declared inputs do not expose files")
}

func (l *InputsContentLoader) GetValues() map[string]any { return l.values }
