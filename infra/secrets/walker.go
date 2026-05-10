package secrets

import (
	"fmt"
	"reflect"
	"strings"
)

// maxWalkDepth is a safety net to keep ExpandStruct / FindMarkers from
// recursing forever through cyclic structures. Real configs are tens
// of levels deep at most.
const maxWalkDepth = 64

// ExpandStruct walks every exported string field of v (recursively
// into structs, slices, maps, pointers) and applies Expand to its
// value. Used for the second-pass PLUGIN resolution after plugins
// boot. Returns the first resolver error encountered; partial
// substitutions made before the error remain in place.
func ExpandStruct(v any) error {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	visited := map[uintptr]struct{}{}
	return walkValue(rv, 0, visited, expandString)
}

// FindMarkers returns every "${PREFIX:ARG}" marker remaining in v's
// reachable string fields, with the given prefix (case-insensitive).
// Used to surface unresolved ${PLUGIN:...} markers as boot errors.
func FindMarkers(v any, prefix string) []string {
	if v == nil {
		return nil
	}
	prefix = strings.ToUpper(prefix)
	var found []string
	visited := map[uintptr]struct{}{}
	_ = walkValue(reflect.ValueOf(v), 0, visited, func(s string) (string, error) {
		for _, m := range scanMarkers(s) {
			if strings.EqualFold(m.prefix, prefix) {
				found = append(found, fmt.Sprintf("${%s:%s}", m.prefix, m.arg))
			}
		}
		return s, nil
	})
	return found
}

type markerHit struct {
	prefix string
	arg    string
}

// scanMarkers returns each ${PREFIX:ARG} found in s. It mirrors the
// shape of expandWith without resolving anything.
func scanMarkers(s string) []markerHit {
	var out []markerHit
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], "${")
		if j < 0 {
			break
		}
		i += j
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			break
		}
		marker := s[i+2 : i+end]
		colon := strings.IndexByte(marker, ':')
		if colon < 0 {
			i += end + 1
			continue
		}
		out = append(out, markerHit{
			prefix: strings.ToUpper(marker[:colon]),
			arg:    marker[colon+1:],
		})
		i += end + 1
	}
	return out
}

// expandString is the per-string callback for ExpandStruct.
func expandString(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	return Expand(s)
}

// walkValue is the reflective traversal. It descends into structs,
// pointers, slices, arrays, and maps. For string-typed leaves it
// applies fn and writes back if the value is settable.
func walkValue(v reflect.Value, depth int, visited map[uintptr]struct{}, fn func(string) (string, error)) error {
	if depth > maxWalkDepth {
		return nil
	}
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		ptr := v.Pointer()
		if _, seen := visited[ptr]; seen {
			return nil
		}
		visited[ptr] = struct{}{}
		return walkValue(v.Elem(), depth+1, visited, fn)

	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		// Only recurse into the concrete element when it's a settable
		// holder (structs, pointers, slices, maps). Atomic interface
		// values can't be reassigned without rebuilding the holder.
		return walkValue(v.Elem(), depth+1, visited, fn)

	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			ft := t.Field(i)
			if !ft.IsExported() {
				continue
			}
			if err := walkValue(v.Field(i), depth+1, visited, fn); err != nil {
				return err
			}
		}
		return nil

	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		// Special-case []byte — treat as opaque, not as a string slice.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := walkValue(v.Index(i), depth+1, visited, fn); err != nil {
				return err
			}
		}
		return nil

	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		iter := v.MapRange()
		for iter.Next() {
			mv := iter.Value()
			// Map values are not addressable; we must rebuild and
			// SetMapIndex if we want to write back. Only rewrite
			// when the value is a string (or pointer/interface
			// wrapping mutable state, which we recurse into).
			switch mv.Kind() {
			case reflect.String:
				old := mv.String()
				newV, err := fn(old)
				if err != nil {
					return err
				}
				if newV != old {
					nv := reflect.New(mv.Type()).Elem()
					nv.SetString(newV)
					v.SetMapIndex(iter.Key(), nv)
				}
			case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
				if err := walkValue(mv, depth+1, visited, fn); err != nil {
					return err
				}
			case reflect.Struct:
				// Map values of struct kind are not addressable. Copy
				// out, walk, write back if changed.
				cp := reflect.New(mv.Type()).Elem()
				cp.Set(mv)
				if err := walkValue(cp, depth+1, visited, fn); err != nil {
					return err
				}
				v.SetMapIndex(iter.Key(), cp)
			}
		}
		return nil

	case reflect.String:
		if !v.CanSet() {
			return nil
		}
		old := v.String()
		newV, err := fn(old)
		if err != nil {
			return err
		}
		if newV != old {
			v.SetString(newV)
		}
		return nil
	}
	return nil
}
