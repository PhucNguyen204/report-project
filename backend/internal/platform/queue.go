package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"time"
)

type Event struct {
	Event     string          `json:"event"`
	Key       string          `json:"key" river:"unique"`
	Payload   json.RawMessage `json:"payload"`
	Encrypted bool            `json:"encrypted,omitempty"`
}

func (Event) Kind() string { return "domain_event" }

type EventHandler func(context.Context, Event) error
type eventWorker struct {
	river.WorkerDefaults[Event]
	handler EventHandler
	cipher  *EventCipher
}

func (w *eventWorker) Work(ctx context.Context, job *river.Job[Event]) error {
	event := job.Args
	if w.cipher != nil {
		var err error
		event, err = w.cipher.Open(event)
		if err != nil {
			return err
		}
	}
	return w.handler(ctx, event)
}
func ConfigureQueue(ctx context.Context, db *DB, handler EventHandler, keys ...[]byte) (*river.Client[pgx.Tx], error) {
	var cipher *EventCipher
	if len(keys) > 1 {
		return nil, fmt.Errorf("only one queue key is supported")
	}
	if len(keys) == 1 {
		var err error
		cipher, err = NewEventCipher(keys[0])
		if err != nil {
			return nil, err
		}
	}
	driver := riverpgxv5.New(db.Pool.Pool)
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
	river.AddWorker(workers, &eventWorker{handler: handler, cipher: cipher})
	client, err := river.NewClient(driver, &river.Config{Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 8}}, Workers: workers})
	if err != nil {
		return nil, err
	}
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, runAt time.Time) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		unique := river.UniqueOpts{ByArgs: true}
		if kind == "assignment.digest" {
			unique.ByState = []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}
		}
		event := Event{Event: kind, Key: kind + ":" + key, Payload: b}
		if cipher != nil {
			event, err = cipher.Seal(event)
			if err != nil {
				return err
			}
		}
		_, err = client.InsertTx(ctx, tx, event, &river.InsertOpts{MaxAttempts: 4, ScheduledAt: runAt, UniqueOpts: unique})
		return err
	}
	return client, nil
}
