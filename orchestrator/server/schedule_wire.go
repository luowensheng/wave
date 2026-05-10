package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"wave/infra/cron"
	"wave/infra/plugins"
)

// startScheduler builds an in-process cron Scheduler from
// Config.Schedule and binds each entry to a plugin invocation.
func (s *Server) startScheduler() error {
	if len(s.Config.Schedule) == 0 {
		return nil
	}
	sched := cron.New()
	for _, j := range s.Config.Schedule {
		if j.Name == "" || j.Plugin == "" {
			return fmt.Errorf("scheduled job missing name/plugin: %+v", j)
		}
		var every time.Duration
		if j.Every != "" {
			d, err := time.ParseDuration(j.Every)
			if err != nil {
				return fmt.Errorf("scheduled job %q every: %w", j.Name, err)
			}
			every = d
		}

		jobName := j.Name
		pluginName := j.Plugin
		trigger := j.TriggerKey
		body := j.Body

		runner := func(ctx context.Context) {
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

		if err := sched.Add(&cron.Job{Name: jobName, Every: every, At: j.At, Run: runner}); err != nil {
			return err
		}
	}
	cron.SetDefault(sched)
	sched.Start(context.Background())
	log.Printf("scheduler started: %d job(s)", len(s.Config.Schedule))
	return nil
}
