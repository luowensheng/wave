// Package secrets resolves indirection markers in YAML strings before
// the orchestrator parses the config.
//
// Supported markers (no escaping; markers must match exactly):
//
//	${ENV:NAME}              → os.Getenv("NAME"); error if unset
//	${ENV:NAME:default}      → os.Getenv("NAME") or "default" if unset
//	${FILE:/path/to/secret}  → file contents (whitespace-trimmed); error if missing
//
// Future markers (Vault, cloud secret managers) plug in via Resolver.
//
// This is intentionally a *string-substitution* layer that runs once at
// config load. We do NOT walk the parsed YAML — that would require knowing
// the field types. Instead we transform the bytes before unmarshal, which
// also means a marker can appear inside any scalar (route paths, URLs,
// auth keys, plugin command lines, etc.).
package secrets

import (
	"fmt"
	"os"
	"strings"
)

// Resolver maps a marker prefix (e.g. "ENV", "FILE") to a resolution fn.
type Resolver func(arg string) (string, error)

var defaultResolvers = map[string]Resolver{
	"ENV":  resolveEnv,
	"FILE": resolveFile,
}

// Register installs a custom resolver. Idempotent: last write wins.
// Useful for downstream code to add Vault/AWS/GCP backends without
// touching this package.
func Register(prefix string, r Resolver) {
	defaultResolvers[strings.ToUpper(prefix)] = r
}

// Expand walks the input, replacing every ${PREFIX:ARG} marker with the
// resolved string. Returns the first resolver error encountered.
//
// Markers that don't match a known prefix are left untouched so the
// orchestrator's existing $arg variable substitution still works.
func Expand(s string) (string, error) {
	return expandWith(s, defaultResolvers)
}

func expandWith(s string, resolvers map[string]Resolver) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		// Look for the next ${
		j := strings.Index(s[i:], "${")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		i += j

		// Find matching }
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			b.WriteString(s[i:])
			break
		}
		marker := s[i+2 : i+end] // strip ${ and }
		colon := strings.IndexByte(marker, ':')
		if colon < 0 {
			// Not a prefix:arg marker — leave intact.
			b.WriteString(s[i : i+end+1])
			i += end + 1
			continue
		}
		prefix := strings.ToUpper(marker[:colon])
		arg := marker[colon+1:]

		fn, ok := resolvers[prefix]
		if !ok {
			b.WriteString(s[i : i+end+1])
			i += end + 1
			continue
		}
		val, err := fn(arg)
		if err != nil {
			return "", fmt.Errorf("${%s:%s}: %w", prefix, arg, err)
		}
		b.WriteString(val)
		i += end + 1
	}
	return b.String(), nil
}

// ── built-in resolvers ────────────────────────────────────────────────────

func resolveEnv(arg string) (string, error) {
	name, def, hasDefault := strings.Cut(arg, ":")
	v := os.Getenv(name)
	if v == "" {
		if hasDefault {
			return def, nil
		}
		return "", fmt.Errorf("env var %q is unset", name)
	}
	return v, nil
}

func resolveFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
