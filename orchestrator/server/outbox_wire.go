package servers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"wave/infra/outbox"
	streampub "wave/usecases/stream_publish"

	_ "github.com/mattn/go-sqlite3"
)

// startOutbox opens the configured SQLite database, builds an outbox
// over it, registers it with stream-publish, and starts the background
// drain worker. Idempotent — re-calling replaces the binding.
func (s *Server) startOutbox() error {
	db, err := sql.Open("sqlite3", s.Config.OutboxDB)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	store, err := outbox.NewSQLiteStore(db)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	ob := outbox.New(store, 10, nil)
	streampub.SetDefaultOutbox(ob)
	ob.Start(context.Background(), time.Second)
	log.Printf("outbox started: db=%s", s.Config.OutboxDB)
	return nil
}
