package fetch

import (
	"fmt"
	"log"
	"net/http"

	"wave/infra/inputs"
	"wave/infra/render"
	"wave/usecases/schedule"
)

// Config is the YAML shape for `type: fetch` routes.
// All fields that produce output are required — no magic defaults.
type Config struct {
	Action              *schedule.Action `yaml:"action,omitempty"`
	Then                []*schedule.Sink `yaml:"then,omitempty"`
	OutputTemplate      string           `yaml:"output_template"`       // required
	ResponseContentType string           `yaml:"response_content_type"` // required
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if c.Action == nil {
		return nil, fmt.Errorf("fetch route %q: action is required", path)
	}
	if c.OutputTemplate == "" {
		return nil, fmt.Errorf("fetch route %q: output_template is required", path)
	}
	if c.ResponseContentType == "" {
		return nil, fmt.Errorf("fetch route %q: response_content_type is required", path)
	}
	if err := schedule.ValidateAction(path, c.Action, c.Then); err != nil {
		return nil, fmt.Errorf("fetch route %q: %w", path, err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Seed accumulator: route inputs go under accum["inputs"].
		accum := make(map[string]any)
		if v := inputs.FromContext(ctx); len(v) > 0 {
			accum["inputs"] = v
		}

		// Execute action.
		result, err := schedule.ExecuteAction(ctx, c.Action, accum)
		if err != nil {
			log.Printf("fetch route %q: action error: %v", path, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Store result under action.Output.
		if c.Action.Output != "" {
			accum[c.Action.Output] = result
		}

		// Apply sinks.
		if err := schedule.ApplySinks(ctx, c.Then, accum); err != nil {
			log.Printf("fetch route %q: sink error: %v", path, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Render output template.
		buf, err := render.Render(c.OutputTemplate, accum)
		if err != nil {
			log.Printf("fetch route %q: template error: %v", path, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", c.ResponseContentType)
		w.Write(buf.Bytes())
	}, nil
}
