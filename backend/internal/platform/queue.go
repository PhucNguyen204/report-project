package platform

import (
	"context"
	"dicho/internal/observability"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/propagation"
	"time"
)

type Event struct {
	Attempt     int             `json:"-"`
	MaxAttempts int             `json:"-"`
	Event       string          `json:"event"`
	Key         string          `json:"key" river:"unique"`
	Payload     json.RawMessage `json:"payload"`
	Encrypted   bool            `json:"encrypted,omitempty"`
	TraceParent string          `json:"traceparent,omitempty"`
	TraceState  string          `json:"tracestate,omitempty"`
}

func (Event) Kind() string { return "domain_event" }

type EventHandler func(context.Context, Event) error
type eventWorker struct {
	river.WorkerDefaults[Event]
	handler  EventHandler
	cipher   *EventCipher
	observer *observability.Observer
}

func (w *eventWorker) Work(ctx context.Context, job *river.Job[Event]) error {
	ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": job.Args.TraceParent, "tracestate": job.Args.TraceState})
	run := func(ctx context.Context) error {
		event := job.Args
		if w.cipher != nil {
			var err error
			event, err = w.cipher.Open(event)
			if err != nil {
				return err
			}
		}
		event.Attempt, event.MaxAttempts = job.Attempt, job.MaxAttempts
		return w.handler(ctx, event)
	}
	if w.observer != nil {
		return w.observer.Work(ctx, job.Args.Event, job.Attempt, run)
	}
	return run(ctx)
}
func ConfigureQueue(ctx context.Context, db *DB, handler EventHandler, keys ...[]byte) (*river.Client[pgx.Tx], error) {
	if err := MigrateQueue(ctx, db.Pool.Pool); err != nil {
		return nil, err
	}
	return OpenQueue(ctx, db, handler, keys...)
}

func MigrateQueue(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func OpenQueue(ctx context.Context, db *DB, handler EventHandler, keys ...[]byte) (*river.Client[pgx.Tx], error) {
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
	if _, err = m.ExistingVersions(ctx); err != nil {
		return nil, err
	}
	validation, err := m.Validate(ctx, nil)
	if err != nil {
		return nil, err
	}
	if !validation.OK {
		return nil, fmt.Errorf("river migrations are pending; run dicho-migrate")
	}
	workers := river.NewWorkers()
	if handler == nil {
		handler = func(_ context.Context, e Event) error { return fmt.Errorf("unhandled event %q", e.Event) }
	}
	river.AddWorker(workers, &eventWorker{handler: handler, cipher: cipher, observer: db.Observer})
	cfg := &river.Config{Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 8}}, Workers: workers}
	if db.Observer != nil {
		cfg.Logger = db.Observer.RiverLogger()
	}
	client, err := river.NewClient(driver, cfg)
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
		carrier := propagation.MapCarrier{}
		propagation.TraceContext{}.Inject(ctx, carrier)
		event.TraceParent, event.TraceState = carrier["traceparent"], carrier["tracestate"]
		if cipher != nil {
			event, err = cipher.Seal(event)
			if err != nil {
				return err
			}
		}
		maxAttempts := 4
		if kind == "ai.suggest" {
			maxAttempts = 2
		}
		if !runAt.After(db.Clock.Now()) {
			runAt = time.Time{}
		}
		_, err = client.InsertTx(ctx, tx, event, &river.InsertOpts{MaxAttempts: maxAttempts, ScheduledAt: runAt, UniqueOpts: unique})
		return err
	}
	return client, nil
}
