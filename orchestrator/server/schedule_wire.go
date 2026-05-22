package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/luowensheng/wave/infra/cron"
	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/usecases/schedule"
)

func (s *Server) startScheduler() error {
	if len(s.Config.Schedule) == 0 {
		return nil
	}
	sched := cron.New()
	for name, j := range s.Config.Schedule {
		// Must have either Plugin (legacy) or Action (new).
		if j.Plugin == "" && j.Action == nil {
			return fmt.Errorf("scheduled job %q: must have plugin or action", name)
		}

		// Boot-time validation: delegated to the schedule package so
		// every sink type (api, for_each, storage, publish, plugin)
		// validates uniformly with the same error messages used by
		// `type: fetch` routes.
		if j.Action != nil {
			if err := schedule.ValidateAction(fmt.Sprintf("scheduled job %q", name), j.Action, j.Then); err != nil {
				return err
			}
		}

		var every time.Duration
		if j.Every != "" {
			d, err := time.ParseDuration(j.Every)
			if err != nil {
				return fmt.Errorf("scheduled job %q every: %w", name, err)
			}
			every = d
		}

		jobName := name // capture loop variable
		job := j        // capture loop variable
		var runner func(ctx context.Context)

		if job.Action != nil {
			runner = func(ctx context.Context) {
				accum := map[string]any{}
				result, err := schedule.ExecuteAction(ctx, job.Action, accum)
				if err != nil {
					log.Printf("scheduled job %q action: %v", jobName, err)
					return
				}
				// Store result under action.Output name so sinks reference
				// it as <output>.<field> (e.g. "tick.msg"). When Output is
				// empty, sinks can still resolve the result's top-level
				// keys directly — but explicit naming is the convention.
				if job.Action.Output != "" {
					accum[job.Action.Output] = result
				} else {
					for k, v := range result {
						accum[k] = v
					}
				}
				if err := schedule.ApplySinks(ctx, job.Then, accum); err != nil {
					log.Printf("scheduled job %q sinks: %v", jobName, err)
				}
			}
		} else {
			// Legacy plugin-only path — no action, no then sinks.
			pluginName := job.Plugin
			trigger := job.TriggerKey
			body := job.Body
			runner = func(ctx context.Context) {
				reg := plugins.Default()
				if reg == nil {
					return
				}
				client, ok := reg.Get(pluginName)
				if !ok {
					log.Printf("scheduled job %q: plugin %q not found", jobName, pluginName)
					return
				}
				b, _ := json.Marshal(body)
				_, err := client.Call(ctx, &plugins.Request{
					TriggerKey: trigger,
					Metadata:   map[string]string{"source": "scheduler", "job_name": jobName},
					Body:       b,
				})
				if err != nil {
					log.Printf("scheduled job %q: %v", jobName, err)
				}
			}
		}

		if err := sched.Add(&cron.Job{Name: jobName, Every: every, At: j.At, Run: runner}); err != nil {
			return err
		}
	}
	cron.SetDefault(sched)
	sched.Start(context.Background())
	log.Printf("scheduler started: %d job(s)", len(s.Config.Schedule))
	return nil
}
