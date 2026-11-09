package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"time"
)

type Event struct {
	Event   string          `json:"event"`
	Key     string          `json:"key" river:"unique"`
	Payload json.RawMessage `json:"payload"`
}

func (Event) Kind() string { return "domain_event" }

type EventHandler func(context.Context, Event) error
type eventWorker struct {
	river.WorkerDefaults[Event]
	handler EventHandler
}

func (w *eventWorker) Work(ctx context.Context, job *river.Job[Event]) error {
	return w.handler(ctx, job.Args)
}
func ConfigureQueue(ctx context.Context, db *DB, handler EventHandler) (*river.Client[pgx.Tx], error) {
	driver := riverpgxv5.New(db.Pool)
	m, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, err
	}
	if _, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	if handler == nil {
		handler = func(_ context.Context, e Event) error { return fmt.Errorf("unhandled event %q", e.Event) }
	}
	river.AddWorker(workers, &eventWorker{handler: handler})
	client, err := river.NewClient(driver, &river.Config{Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 8}}, Workers: workers})
	if err != nil {
		return nil, err
	}
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, runAt time.Time) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = client.InsertTx(ctx, tx, Event{Event: kind, Key: kind + ":" + key, Payload: b}, &river.InsertOpts{MaxAttempts: 4, ScheduledAt: runAt, UniqueOpts: river.UniqueOpts{ByArgs: true}})
		return err
	}
	return client, nil
}
