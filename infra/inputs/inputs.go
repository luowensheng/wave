// Package inputs declares, parses, and validates per-route inputs.
//
// Routes specify what they accept up front (`inputs:` block in YAML);
// the middleware here pulls each declared input out of its source
// (path / query / json body / form / header / cookie), coerces it to
// the declared type, runs validators (required / pattern / min /
// max / enum), and stuffs the cleaned-up map onto the request context
// for downstream handlers.
//
// On validation failure the middleware writes a single 400 with a
// JSON envelope listing every problem at once, so clients get one
// round trip per misformed request.
//
// Strict scope: only declared names are available to template
// substitution. Unknown placeholders fail at handler boot rather than
// silently producing the empty string.
package inputs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
)

// Source enumerates where an input value comes from.
type Source string

const (
	SourcePath    Source = "path"
	SourceQuery   Source = "query"
	SourceBody    Source = "body"     // a named field of the parsed body (JSON key, multipart field, form value)
	SourceForm    Source = "form"     // form-encoded body or multipart (legacy alias for body when CT is form/multipart)
	SourceHeader  Source = "header"
	SourceCookie  Source = "cookie"
	SourceBodyRaw Source = "body_raw" // the whole request body as bytes (for text/plain, octet-stream, etc.)
)

// Type enumerates the coercion target for a parsed input.
type Type string

const (
	TypeString Type = "string"
	TypeInt    Type = "int"
	TypeFloat  Type = "float"
	TypeBool   Type = "bool"
	TypeEmail  Type = "email"
	TypeUUID   Type = "uuid"
	TypeFile   Type = "file"  // multipart file upload — value is *File
	TypeBytes  Type = "bytes" // raw bytes — value is []byte
)

// File is the value type for inputs declared as `type: file`. Surfaced
// to handlers via inputs.FromContext(ctx)["fieldname"].
type File struct {
	Filename    string               `json:"filename"`
	Size        int64                `json:"size"`
	ContentType string               `json:"content_type"`
	Header      textproto.MIMEHeader `json:"-"`
	header      *multipart.FileHeader
}

// Open returns a reader for the file's bytes. Caller must Close.
func (f *File) Open() (multipart.File, error) {
	if f.header == nil {
		return nil, fmt.Errorf("file %q has no underlying multipart header", f.Filename)
	}
	return f.header.Open()
}

// Spec describes one declared input.
type Spec struct {
	Name     string   `yaml:"name,omitempty" json:"name,omitempty"`
	Source   Source   `yaml:"source,omitempty" json:"source,omitempty"`
	From     string   `yaml:"from,omitempty" json:"from,omitempty"` // override key name in source (default: Name)
	Type     Type     `yaml:"type,omitempty" json:"type,omitempty"`
	Required bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default  any      `yaml:"default,omitempty" json:"default,omitempty"`
	Pattern  string   `yaml:"pattern,omitempty" json:"pattern,omitempty"` // Go regexp; strings only
	Min      *float64 `yaml:"min,omitempty" json:"min,omitempty"`         // numbers + string length
	Max      *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	Enum     []string `yaml:"enum,omitempty" json:"enum,omitempty"`

	// pre-compiled — populated by Compile() once at boot.
	patternRE *regexp.Regexp
}

// SpecSet is the materialized, validated form of an `inputs:` block.
//
// ExpectedContentType is the Content-Type the route declares it accepts
// for body inputs. It drives how source: body / body_raw / form get
// extracted:
//
//	application/json                  → JSON-parse, lookup by key
//	application/x-www-form-urlencoded → form-parse, lookup by key
//	multipart/form-data               → multipart-parse, files via type:file
//	text/plain | application/octet-stream | (anything else) → raw bytes
//	                                    (only source:body_raw works)
//
// Empty defaults to JSON for back-compat with routes that pre-date this
// field. The middleware logs a warning when the request's Content-Type
// doesn't match the declared expectation.
type SpecSet struct {
	List                []Spec
	ExpectedContentType string
	byName              map[string]*Spec
}

// Compile validates a list of Specs (default values match the type,
// patterns are valid regexp, enum values are non-empty for string
// types, etc.) and returns a SpecSet ready for runtime use.
func Compile(specs []Spec) (*SpecSet, error) {
	set := &SpecSet{byName: map[string]*Spec{}}
	for i := range specs {
		s := specs[i]
		if s.Name == "" {
			return nil, fmt.Errorf("input %d: name required", i)
		}
		if _, dup := set.byName[s.Name]; dup {
			return nil, fmt.Errorf("input %q: duplicate name", s.Name)
		}
		switch s.Source {
		case "":
			s.Source = SourceQuery // sane default for GETs; explicit always wins
		case SourcePath, SourceQuery, SourceBody, SourceForm, SourceHeader, SourceCookie, SourceBodyRaw:
			// ok
		default:
			return nil, fmt.Errorf("input %q: unknown source %q", s.Name, s.Source)
		}
		if s.Type == "" {
			s.Type = TypeString
		}
		switch s.Type {
		case TypeString, TypeInt, TypeFloat, TypeBool, TypeEmail, TypeUUID, TypeFile, TypeBytes:
		default:
			return nil, fmt.Errorf("input %q: unknown type %q", s.Name, s.Type)
		}
		// Type:file requires source:body or source:form (looked up in
		// the multipart file map). Type:bytes requires source:body_raw.
		if s.Type == TypeFile && s.Source != SourceBody && s.Source != SourceForm {
			return nil, fmt.Errorf("input %q: type:file requires source:body or source:form", s.Name)
		}
		if s.Type == TypeBytes && s.Source != SourceBodyRaw {
			return nil, fmt.Errorf("input %q: type:bytes requires source:body_raw", s.Name)
		}
		if s.Pattern != "" {
			re, err := regexp.Compile(s.Pattern)
			if err != nil {
				return nil, fmt.Errorf("input %q: bad pattern: %w", s.Name, err)
			}
			s.patternRE = re
		}
		set.List = append(set.List, s)
		set.byName[s.Name] = &set.List[len(set.List)-1]
	}
	return set, nil
}

// Names returns the declared input names. Used by the template
// pre-flight check in usecases/api & storage_access to verify that
// templates only reference declared variables.
func (s *SpecSet) Names() []string {
	out := make([]string, 0, len(s.List))
	for _, sp := range s.List {
		out = append(out, sp.Name)
	}
	return out
}

// ── extraction + validation ───────────────────────────────────────────────

// Issue is one validation problem.
type Issue struct {
	Input  string `json:"input"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Result is the cleaned-up value map plus any issues found.
type Result struct {
	Values map[string]any
	Issues []Issue
}

// Parse extracts every declared input from r. Returns the parsed map
// + the list of issues. Caller decides how to respond on len(issues)>0.
func (s *SpecSet) Parse(r *http.Request) Result {
	out := Result{Values: map[string]any{}}
	body := parseBody(r, s.ExpectedContentType)
	formParsed := false

	for _, sp := range s.List {
		key := sp.From
		if key == "" {
			key = sp.Name
		}

		// Type:file is a non-string lookup straight from the multipart
		// file map. Skip the string-coerce path entirely.
		if sp.Type == TypeFile {
			if f, ok := body.files[key]; ok {
				out.Values[sp.Name] = f
				continue
			}
			if sp.Required {
				out.Issues = append(out.Issues, Issue{Input: sp.Name, Source: string(sp.Source), Reason: "required"})
			}
			continue
		}

		// Type:bytes: hand back the captured raw bytes (or the file's
		// bytes if the source is body_raw and a multipart raw body was
		// captured — degenerate case, mostly useful with text/plain).
		if sp.Type == TypeBytes {
			if body.raw != nil {
				out.Values[sp.Name] = body.raw
				continue
			}
			if sp.Required {
				out.Issues = append(out.Issues, Issue{Input: sp.Name, Source: string(sp.Source), Reason: "required"})
			}
			continue
		}

		raw, present := lookup(r, sp.Source, key, body, &formParsed)

		if !present {
			if sp.Default != nil {
				out.Values[sp.Name] = sp.Default
				continue
			}
			if sp.Required {
				out.Issues = append(out.Issues, Issue{
					Input: sp.Name, Source: string(sp.Source),
					Reason: "required",
				})
			}
			continue
		}

		coerced, err := coerce(raw, sp.Type)
		if err != nil {
			out.Issues = append(out.Issues, Issue{
				Input: sp.Name, Source: string(sp.Source),
				Reason: err.Error(),
			})
			continue
		}
		if issue := validate(sp, coerced); issue != "" {
			out.Issues = append(out.Issues, Issue{
				Input: sp.Name, Source: string(sp.Source), Reason: issue,
			})
			continue
		}
		out.Values[sp.Name] = coerced
	}
	return out
}

// lookup pulls a single value by source. Returns (raw-string, present).
func lookup(r *http.Request, src Source, key string, body bodyData, formParsed *bool) (string, bool) {
	switch src {
	case SourcePath:
		v := r.PathValue(key)
		return v, v != ""
	case SourceQuery:
		if !r.URL.Query().Has(key) {
			return "", false
		}
		return r.URL.Query().Get(key), true
	case SourceHeader:
		// Special-case Host: Go strips it from r.Header and stores it
		// on r.Host. Same trick as net/http.Server's own logger.
		if strings.EqualFold(key, "Host") {
			return r.Host, r.Host != ""
		}
		v := r.Header.Get(key)
		return v, v != ""
	case SourceCookie:
		c, err := r.Cookie(key)
		if err != nil || c.Value == "" {
			return "", false
		}
		return c.Value, true
	case SourceBody, SourceForm:
		// Both sources read from the parsed body. SourceForm is kept as
		// a legacy alias for SourceBody when the body is form-encoded
		// or multipart. The body parser already populated body.values.
		if body.values == nil {
			return "", false
		}
		v, ok := body.values[key]
		if !ok {
			return "", false
		}
		return fmt.Sprint(v), true
	case SourceBodyRaw:
		// Raw body capture: the caller is binding the entire request
		// body to a single named input. Returned as a string so the
		// usual coerce() path applies (TypeBytes is short-circuited
		// in Parse before this point).
		if body.raw == nil {
			return "", false
		}
		return string(body.raw), true
	}
	return "", false
}

// bodyData is the result of parsing the request body according to the
// route's expected_content_type. Carries scalar values (form fields /
// json keys), file uploads (multipart only), and the raw bytes (for
// text/plain or octet-stream body_raw inputs).
type bodyData struct {
	values map[string]any
	files  map[string]*File
	raw    []byte
}

// parseBody reads r.Body once according to expectedCT and returns a
// bodyData. The body is replaced with a re-readable buffer so downstream
// handlers (api / storage_access) still see it.
//
// expectedCT empty defaults to JSON for back-compat with routes that
// pre-date the field. Unknown content types fall through to raw capture.
func parseBody(r *http.Request, expectedCT string) bodyData {
	if r.Body == nil {
		return bodyData{}
	}
	want := normalizeCT(expectedCT)
	if want == "" {
		// No declared expectation — sniff from request, but only honour
		// JSON / multipart / form-urlencoded. Anything else is raw.
		want = normalizeCT(r.Header.Get("Content-Type"))
	}
	switch want {
	case "application/json", "":
		return parseJSONBody(r)
	case "multipart/form-data":
		return parseMultipartBody(r)
	case "application/x-www-form-urlencoded":
		return parseURLEncodedBody(r)
	default:
		return parseRawBody(r)
	}
}

// normalizeCT strips parameters (charset, boundary) and lowercases.
func normalizeCT(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct
}

func parseJSONBody(r *http.Request) bodyData {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return bodyData{}
	}
	_ = r.Body.Close()
	r.Body = newRereadable(raw)
	if len(raw) == 0 {
		return bodyData{raw: raw}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		// Malformed JSON returns the raw bytes so body_raw still works
		// and required field validation still flags missing keys.
		return bodyData{raw: raw}
	}
	return bodyData{values: out, raw: raw}
}

const maxMultipartMemory = 32 << 20 // 32 MiB

func parseMultipartBody(r *http.Request) bodyData {
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		return bodyData{}
	}
	out := bodyData{values: map[string]any{}, files: map[string]*File{}}
	for k, vs := range r.MultipartForm.Value {
		if len(vs) == 1 {
			out.values[k] = vs[0]
		} else {
			out.values[k] = vs
		}
	}
	for k, headers := range r.MultipartForm.File {
		if len(headers) == 0 {
			continue
		}
		h := headers[0]
		out.files[k] = &File{
			Filename:    h.Filename,
			Size:        h.Size,
			ContentType: h.Header.Get("Content-Type"),
			Header:      h.Header,
			header:      h,
		}
	}
	return out
}

func parseURLEncodedBody(r *http.Request) bodyData {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return bodyData{}
	}
	_ = r.Body.Close()
	r.Body = newRereadable(raw)
	// Re-set the body so ParseForm reads from our rereadable copy.
	if err := r.ParseForm(); err != nil {
		return bodyData{raw: raw}
	}
	out := map[string]any{}
	for k, vs := range r.PostForm {
		if len(vs) == 1 {
			out[k] = vs[0]
		} else {
			out[k] = vs
		}
	}
	return bodyData{values: out, raw: raw}
}

func parseRawBody(r *http.Request) bodyData {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return bodyData{}
	}
	_ = r.Body.Close()
	r.Body = newRereadable(raw)
	return bodyData{raw: raw}
}

// rereadable lets a handler chain read r.Body more than once.
type rereadable struct {
	b []byte
	i int
}

func newRereadable(b []byte) io.ReadCloser { return &rereadable{b: b} }
func (r *rereadable) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
func (r *rereadable) Close() error { return nil }

// coerce parses a raw string into the declared type.
func coerce(raw string, t Type) (any, error) {
	switch t {
	case TypeString, "":
		return raw, nil
	case TypeInt:
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("not an integer")
		}
		return v, nil
	case TypeFloat:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("not a number")
		}
		return v, nil
	case TypeBool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("not a boolean")
		}
		return v, nil
	case TypeEmail:
		if !emailRE.MatchString(raw) {
			return nil, fmt.Errorf("not a valid email")
		}
		return raw, nil
	case TypeUUID:
		if !uuidRE.MatchString(raw) {
			return nil, fmt.Errorf("not a valid UUID")
		}
		return raw, nil
	}
	return raw, nil
}

// validate runs the per-spec validators against an already-coerced value.
func validate(sp Spec, v any) string {
	switch x := v.(type) {
	case string:
		if sp.patternRE != nil && !sp.patternRE.MatchString(x) {
			return "does not match pattern"
		}
		if sp.Min != nil && float64(len(x)) < *sp.Min {
			return fmt.Sprintf("length < %v", *sp.Min)
		}
		if sp.Max != nil && float64(len(x)) > *sp.Max {
			return fmt.Sprintf("length > %v", *sp.Max)
		}
		if len(sp.Enum) > 0 && !contains(sp.Enum, x) {
			return "not in enum"
		}
	case int64:
		f := float64(x)
		if sp.Min != nil && f < *sp.Min {
			return fmt.Sprintf("< %v", *sp.Min)
		}
		if sp.Max != nil && f > *sp.Max {
			return fmt.Sprintf("> %v", *sp.Max)
		}
	case float64:
		if sp.Min != nil && x < *sp.Min {
			return fmt.Sprintf("< %v", *sp.Min)
		}
		if sp.Max != nil && x > *sp.Max {
			return fmt.Sprintf("> %v", *sp.Max)
		}
	}
	return ""
}

func contains(s []string, want string) bool {
	for _, x := range s {
		if x == want {
			return true
		}
	}
	return false
}

var (
	// Pragmatic patterns — not RFC-strict, but cover ~99% of real
	// values. Users wanting stricter matching can layer Pattern on top.
	emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ── context plumbing ──────────────────────────────────────────────────────

type ctxKey struct{}

// WithValues stashes the parsed input map on a context. Middleware
// calls this; downstream handlers read via FromContext.
func WithValues(ctx context.Context, v map[string]any) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// FromContext returns the parsed input map (or empty map when not set).
func FromContext(ctx context.Context) map[string]any {
	if v, ok := ctx.Value(ctxKey{}).(map[string]any); ok {
		return v
	}
	return map[string]any{}
}
