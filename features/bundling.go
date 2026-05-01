package features

// Bundling is the capability of building and watching JavaScript bundles
// with cache busting and template processing. The orchestrator wires
// concrete closures backed by infra/bundler at startup.
type Bundling struct {
	Build func() error
	Watch func() error
}
