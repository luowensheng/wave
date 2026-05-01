package features

// Rendering is the capability of converting markdown/text to HTML and
// applying response templates. The orchestrator wires concrete closures
// backed by infra/render at startup.
type Rendering struct {
	MarkdownToHTML func(input []byte) []byte
	ApplyTemplate  func(name string, data any) ([]byte, error)
}
