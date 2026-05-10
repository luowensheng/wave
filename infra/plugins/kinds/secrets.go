package kinds

import "context"

// JSON-RPC method names exposed by secrets-kind plugins.
const (
	MethodSecretsResolve = "secrets.resolve"
)

// SecretsPlugin is the typed interface for KindSecrets plugins. It
// mirrors the existing infra/secrets.Resolver shape but with full
// context propagation, suitable for remote backends (Vault, AWS).
type SecretsPlugin interface {
	Resolve(ctx context.Context, uri string) ([]byte, error)
	Close() error
}
