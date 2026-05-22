package schedule

// Action is the outbound operation to perform (api call, plugin call, or storage query).
// Output names the key under which the result is stored in the accumulator.
// Output is required when Then is non-empty.
type Action struct {
	Type       string            `yaml:"type,omitempty"`        // api | plugin | storage
	Output     string            `yaml:"output,omitempty"`      // accumulator key for result
	Ref        string            `yaml:"ref,omitempty"`         // named request from requests:
	Vars       map[string]string `yaml:"vars,omitempty"`        // template var → dot-path into accum
	URL        string            `yaml:"url,omitempty"`
	Method     string            `yaml:"method,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Body       string            `yaml:"body,omitempty"`
	Plugin     string            `yaml:"plugin,omitempty"`
	TriggerKey string            `yaml:"trigger_key,omitempty"`
	Source     string            `yaml:"source,omitempty"`      // for type=storage
	Execute    string            `yaml:"execute,omitempty"`     // for type=storage
}

// Sink is applied after the action completes, using the accumulator.
// Inputs maps SQL template name → dot-path into the accumulator.
// All values must be explicit <namespace>.<field> paths — no empty strings.
type Sink struct {
	Type       string            `yaml:"type,omitempty"` // storage | publish | plugin | api | for_each
	Source     string            `yaml:"source,omitempty"`
	Inputs     map[string]string `yaml:"inputs,omitempty"`
	Execute    string            `yaml:"execute,omitempty"`
	Connection string            `yaml:"connection,omitempty"`
	EventType  string            `yaml:"event_type,omitempty"`
	Plugin     string            `yaml:"plugin,omitempty"`
	TriggerKey string            `yaml:"trigger_key,omitempty"`

	// api sink fields (mirror Action's api fields)
	Ref     string            `yaml:"ref,omitempty"`    // named request from requests:
	Vars    map[string]string `yaml:"vars,omitempty"`   // template var → dot-path into accum
	URL     string            `yaml:"url,omitempty"`    // inline url
	Method  string            `yaml:"method,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`
	Output  string            `yaml:"output,omitempty"` // store result in accum[output]

	// for_each sink fields
	In string  `yaml:"in,omitempty"` // dot-path into accum to an array (e.g. "peek.data")
	As string  `yaml:"as,omitempty"` // accumulator key for each element (e.g. "item")
	Do []*Sink `yaml:"do,omitempty"` // nested sinks run per element

	// OnError controls failure handling for THIS sink (any type, incl.
	// for_each). It does NOT mask errors in sibling/parent sinks.
	//   "" | "abort"        → propagate the error (default: aborts the tick)
	//   "continue" | "skip" → log a warning and skip THIS sink; sibling
	//                          sinks (and the enclosing for_each loop) keep
	//                          running. Use for best-effort ingest where a
	//                          missing path (e.g. an Atom feed has no
	//                          rss.channel.item) must not stall the job.
	OnError string `yaml:"on_error,omitempty"`
}
