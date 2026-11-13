package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (o *Observer) Work(ctx context.Context, kind string, attempt int, run func(context.Context) error) (err error) {
	switch kind {
	case "email", "notification", "settings.changed", "space.copy", "space.purge", "account.purge", "account.anonymized", "food.merged", "config.changed", "report.export", "media.delete", "export.delete", "assignment.digest":
	default:
		kind = "other"
	}
	ctx, span := o.tracer.Start(ctx, "river "+kind, trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("job.kind", kind), attribute.Int("job.attempt", attempt)))
	started := time.Now()
	o.activeAttempts.Inc()
	defer func() {
		panicked := recover()
		result := "ok"
		if panicked != nil {
			result = "panic"
		} else if err != nil {
			result = "error"
		}
		duration := time.Since(started)
		o.activeAttempts.Dec()
		o.attempts.WithLabelValues(kind, result).Inc()
		o.attemptDuration.WithLabelValues(kind, result).Observe(duration.Seconds())
		if result != "ok" {
			span.SetStatus(codes.Error, "job failed")
		}
		o.logger.InfoContext(ctx, "job attempt", "trace_id", traceID(ctx), "kind", kind, "attempt", attempt, "result", result, "duration_ms", float64(duration)/float64(time.Millisecond))
		span.End()
		if panicked != nil {
			panic(panicked)
		}
	}()
	return run(ctx)
}
