package plugins

// Plugin kind constants. These select which typed contract a plugin
// implements. The empty string defaults to KindHandler for back-compat
// with the original Request/Response shape.
const (
	// KindHandler is the original Request/Response shape used by the
	// `type: plugin` route.
	KindHandler = "handler"
	// KindStorage is a key/value + SQL-ish storage adapter.
	KindStorage = "storage"
	// KindAuth is an authentication / identity provider.
	KindAuth = "auth"
	// KindSecrets is a secret resolver (e.g. Vault, AWS Secrets Manager).
	KindSecrets = "secrets"
	// KindExporter is an observability exporter (metrics/traces/logs).
	KindExporter = "exporter"
)

// IsKnownKind reports whether s is a recognized plugin kind. Empty
// string is also accepted (treated as KindHandler).
func IsKnownKind(s string) bool {
	switch s {
	case "", KindHandler, KindStorage, KindAuth, KindSecrets, KindExporter:
		return true
	}
	return false
}

// EffectiveKind returns the configured kind, defaulting to KindHandler
// when the field is empty.
func EffectiveKind(s string) string {
	if s == "" {
		return KindHandler
	}
	return s
}
