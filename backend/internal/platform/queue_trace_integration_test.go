//go:build integration

package platform_test

import (
	"bytes"
	"context"
	"dicho/internal/app"
	"dicho/internal/observability"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestEncryptedQueuePreservesAPITraceAcrossRetryAndLegacyJobs(t *testing.T) {
	db := testutil.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	var logs bytes.Buffer
	o := observability.New(observability.Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), TracerProvider: provider})
	db.Observer = o
	var calls atomic.Int32
	client, err := platform.ConfigureQueue(ctx, db, func(ctx context.Context, event platform.Event) error {
		if !bytes.Contains(event.Payload, []byte("PRIVATE-PAYLOAD")) || !trace.SpanContextFromContext(ctx).IsValid() {
			return errors.New("missing decrypted payload or trace")
		}
		if calls.Add(1) == 1 {
			return errors.New("PRIVATE-PROVIDER-ERROR")
		}
		return nil
	}, bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := client.Subscribe(river.EventKindJobFailed, river.EventKindJobCompleted)
	defer unsubscribe()
	handler := app.NewHandler(db, nil, app.Config{Observer: o}, func(m *http.ServeMux) {
		m.HandleFunc("POST /it4788/v1/auth/register", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
			if err := db.Tx(r.Context(), func(tx pgx.Tx) error {
				return db.Enqueue(r.Context(), tx, "email", platform.ID(), map[string]string{"secret": "PRIVATE-PAYLOAD"}, time.Now())
			}); err != nil {
				return err
			}
			platform.Write(w, 202, map[string]bool{"queued": true})
			return nil
		}))
	})
	r := httptest.NewRequest("POST", "/it4788/v1/auth/register", nil)
	r.Header.Set("X-App-Version", "1.0.0")
	r.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("gateway enqueue status%d", w.Code)
	}
	var id int64
	var raw string
	if err := db.Pool.QueryRow(ctx, "SELECT id,args::text FROM river_job").Scan(&id, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "traceparent") || !strings.Contains(raw, "11111111111111111111111111111111") || strings.Contains(raw, "PRIVATE-PAYLOAD") {
		t.Fatal("queue lost trace context or persisted plaintext payload")
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	wait := func(kind river.EventKind) {
		t.Helper()
		select {
		case event := <-events:
			if event.Kind != kind {
				t.Fatalf("job event=%s want=%s", event.Kind, kind)
			}
		case <-ctx.Done():
			t.Fatal("worker did not finish attempt")
		}
	}
	wait(river.EventKindJobFailed)
	if _, err := client.JobRetry(ctx, id); err != nil {
		t.Fatal(err)
	}
	wait(river.EventKindJobCompleted)
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return db.Enqueue(ctx, tx, "email", "legacy", map[string]string{"secret": "PRIVATE-PAYLOAD"}, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	wait(river.EventKindJobCompleted)
	if err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || strings.Contains(logs.String(), "PRIVATE") {
		t.Fatal("worker retry/legacy behavior changed or telemetry leaked")
	}
	spans := exporter.GetSpans()
	if len(spans) != 4 || spans[0].SpanKind != trace.SpanKindServer {
		t.Fatalf("span count%d", len(spans))
	}
	for _, span := range spans[1:3] {
		if span.SpanKind != trace.SpanKindConsumer || span.SpanContext.TraceID() != spans[0].SpanContext.TraceID() || span.Parent.SpanID() != spans[0].SpanContext.SpanID() {
			t.Fatal("worker attempt not linked to API span")
		}
	}
	if spans[1].SpanContext.SpanID() == spans[2].SpanContext.SpanID() || spans[3].Parent.IsValid() {
		t.Fatal("attempt reused span or legacy job inherited unrelated trace")
	}
}
