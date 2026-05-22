package match

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// compiledCase is one fully-prepared case: a predicate function and
// the wrapped sub-handler to invoke if the predicate returns true.
type compiledCase struct {
	eval    func(*http.Request) bool
	handler http.HandlerFunc
}

// compileCase turns a YAML Case into a compiledCase. Runs at boot;
// regex compile failures and shape errors fail here so the server
// never starts with a broken matcher.
func compileCase(c Case, where string) (compiledCase, error) {
	if c.When == "" {
		return compiledCase{}, fmt.Errorf("match: %s: missing `when`", where)
	}
	if c.Match == nil {
		return compiledCase{}, fmt.Errorf("match: %s: missing `match`", where)
	}
	if c.Route == nil {
		return compiledCase{}, fmt.Errorf("match: %s: missing `route`", where)
	}

	eval, err := compilePredicate(c.When, c.Match, where)
	if err != nil {
		return compiledCase{}, err
	}

	handler, err := BuildSubHandlerFn(c.Route)
	if err != nil {
		return compiledCase{}, fmt.Errorf("match: %s: route: %w", where, err)
	}

	return compiledCase{eval: eval, handler: handler}, nil
}

// compilePredicate builds a request → bool function for one (when,
// match) pair. Returns an error on shape mismatches or invalid
// operator values.
func compilePredicate(when string, match any, where string) (func(*http.Request) bool, error) {
	switch when {
	case "method":
		op, err := asOp(match, where+".match")
		if err != nil {
			return nil, err
		}
		eval, err := compileOp(op, where+".match", true /* upper-case the candidate */)
		if err != nil {
			return nil, err
		}
		return func(r *http.Request) bool { return eval(r.Method) }, nil

	case "host":
		op, err := asOp(match, where+".match")
		if err != nil {
			return nil, err
		}
		eval, err := compileOp(op, where+".match", true)
		if err != nil {
			return nil, err
		}
		return func(r *http.Request) bool { return eval(r.Host) }, nil

	case "ip":
		op, err := asOp(match, where+".match")
		if err != nil {
			return nil, err
		}
		eval, err := compileOp(op, where+".match", false)
		if err != nil {
			return nil, err
		}
		return func(r *http.Request) bool { return eval(clientIP(r)) }, nil

	case "header":
		return compileKeyed(match, where, func(r *http.Request, key string) (string, bool) {
			v := r.Header.Get(key)
			return v, v != ""
		})

	case "cookie":
		return compileKeyed(match, where, func(r *http.Request, key string) (string, bool) {
			c, err := r.Cookie(key)
			if err != nil || c == nil {
				return "", false
			}
			return c.Value, c.Value != ""
		})

	case "query":
		return compileKeyed(match, where, func(r *http.Request, key string) (string, bool) {
			q := r.URL.Query()
			if !q.Has(key) {
				return "", false
			}
			v := q.Get(key)
			return v, true
		})

	case "path":
		return compileKeyed(match, where, func(r *http.Request, key string) (string, bool) {
			v := r.PathValue(key)
			return v, v != ""
		})

	default:
		return nil, fmt.Errorf("match: %s: unknown `when` dimension %q (expected method|host|ip|header|cookie|query|path)", where, when)
	}
}

// compileKeyed handles header/cookie/query/path: match is
// map[string]any, each value is string or MatchOp. All keys AND.
func compileKeyed(match any, where string, lookup func(*http.Request, string) (string, bool)) (func(*http.Request) bool, error) {
	m, err := asMap(match, where+".match")
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("match: %s.match: must have at least one key", where)
	}
	type compiledKey struct {
		key  string
		eval func(value string, present bool) bool
	}
	keys := make([]compiledKey, 0, len(m))
	for k, v := range m {
		op, err := asOp(v, fmt.Sprintf("%s.match[%q]", where, k))
		if err != nil {
			return nil, err
		}
		valueEval, err := compileOp(op, fmt.Sprintf("%s.match[%q]", where, k), false)
		if err != nil {
			return nil, err
		}
		existsCheck := op.Exists
		keys = append(keys, compiledKey{
			key: k,
			eval: func(value string, present bool) bool {
				if existsCheck != nil {
					return present == *existsCheck
				}
				if !present {
					return false
				}
				return valueEval(value)
			},
		})
	}
	return func(r *http.Request) bool {
		for _, ck := range keys {
			v, present := lookup(r, ck.key)
			if !ck.eval(v, present) {
				return false
			}
		}
		return true
	}, nil
}

// asMap normalizes a YAML-decoded map (map[string]any or
// map[any]any) into map[string]any. Returns an error if the value
// isn't map-shaped.
func asMap(v any, where string) (map[string]any, error) {
	switch m := v.(type) {
	case map[string]any:
		return m, nil
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("%s: non-string key %v", where, k)
			}
			out[ks] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s: expected a map, got %T", where, v)
	}
}

// asOp coerces a value into a MatchOp. Plain strings become
// { Equals: s }. Maps with operator keys are decoded.
func asOp(v any, where string) (MatchOp, error) {
	switch x := v.(type) {
	case string:
		return MatchOp{Equals: x}, nil
	case bool:
		// Allow `match: true` / `match: false` as a shorthand for exists.
		b := x
		return MatchOp{Exists: &b}, nil
	}
	m, err := asMap(v, where)
	if err != nil {
		// Not a map and not a string → invalid shape.
		return MatchOp{}, fmt.Errorf("%s: must be a string or an operator object (equals/regex/prefix/exists), got %T", where, v)
	}
	var op MatchOp
	for k, val := range m {
		switch strings.ToLower(k) {
		case "equals":
			s, ok := val.(string)
			if !ok {
				return MatchOp{}, fmt.Errorf("%s.equals: must be a string", where)
			}
			op.Equals = s
		case "regex":
			s, ok := val.(string)
			if !ok {
				return MatchOp{}, fmt.Errorf("%s.regex: must be a string", where)
			}
			op.Regex = s
		case "prefix":
			s, ok := val.(string)
			if !ok {
				return MatchOp{}, fmt.Errorf("%s.prefix: must be a string", where)
			}
			op.Prefix = s
		case "exists":
			b, ok := val.(bool)
			if !ok {
				return MatchOp{}, fmt.Errorf("%s.exists: must be a bool", where)
			}
			op.Exists = &b
		default:
			return MatchOp{}, fmt.Errorf("%s: unknown operator %q (allowed: equals, regex, prefix, exists)", where, k)
		}
	}
	if op.Equals == "" && op.Regex == "" && op.Prefix == "" && op.Exists == nil {
		return MatchOp{}, fmt.Errorf("%s: at least one of equals/regex/prefix/exists must be set", where)
	}
	return op, nil
}

// compileOp turns a MatchOp into a string → bool function.
//
// upperCase=true forces the candidate to upper case before comparing
// — used for method/host where case shouldn't matter. Note: regex
// receives the candidate as-is (the user can use (?i) if they want
// case-insensitive match).
func compileOp(op MatchOp, where string, upperCase bool) (func(string) bool, error) {
	if op.Exists != nil {
		// Exists is handled at the outer keyed level. For scalar
		// dimensions (method/host/ip), Exists doesn't really apply —
		// those values are always present on a Request. Treat
		// exists:true as match-anything and exists:false as no-match.
		want := *op.Exists
		return func(string) bool { return want }, nil
	}
	if op.Regex != "" {
		re, err := regexp.Compile(op.Regex)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid regex %q: %w", where, op.Regex, err)
		}
		return func(s string) bool {
			if upperCase {
				s = strings.ToUpper(s)
			}
			return re.MatchString(s)
		}, nil
	}
	if op.Prefix != "" {
		want := op.Prefix
		if upperCase {
			want = strings.ToUpper(want)
		}
		return func(s string) bool {
			if upperCase {
				s = strings.ToUpper(s)
			}
			return strings.HasPrefix(s, want)
		}, nil
	}
	// Default: equals.
	want := op.Equals
	if upperCase {
		want = strings.ToUpper(want)
	}
	return func(s string) bool {
		if upperCase {
			s = strings.ToUpper(s)
		}
		return s == want
	}, nil
}

// clientIP extracts a best-guess client IP for the `ip` dimension.
// Uses X-Forwarded-For first hop, then X-Real-IP, then RemoteAddr.
// (No infrahttp dependency here to keep the package import-light.)
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
