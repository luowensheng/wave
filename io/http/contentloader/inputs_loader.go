package contentloader

import (
	"fmt"
	"io"
	"net/http"

	"wave/infra/inputs"
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

// GetFile surfaces an *inputs.File (declared as `type: file` in the
// route's inputs block) as the contentloader.File shape that the
// filesystem storage backend's WRITE template helper expects.
// Returns an error if the named input wasn't declared as a file.
func (l *InputsContentLoader) GetFile(name string) (*File, error) {
	raw, ok := l.values[name]
	if !ok {
		return nil, fmt.Errorf("undeclared input %q", name)
	}
	switch f := raw.(type) {
	case *inputs.File:
		if f == nil {
			return nil, fmt.Errorf("input %q: nil file", name)
		}
		return &File{
			Filename: f.Filename,
			Size:     f.Size,
			Reader:   &inputsFileReader{f: f},
		}, nil
	case *File:
		return f, nil
	default:
		return nil, fmt.Errorf("input %q is not a file (type %T)", name, raw)
	}
}

func (l *InputsContentLoader) GetValues() map[string]any { return l.values }

// inputsFileReader bridges *inputs.File (multipart-backed) into the
// domain.FileReader interface so filesystem.WRITE can stream the bytes
// to disk without copying through memory.
type inputsFileReader struct {
	f *inputs.File
}

func (r *inputsFileReader) Open() (io.ReadCloser, error) {
	mf, err := r.f.Open()
	if err != nil {
		return nil, err
	}
	return mf, nil
}
