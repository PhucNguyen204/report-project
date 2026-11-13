package observability

import (
	"context"
	"log/slog"
)

func (o *Observer) RiverLogger() *slog.Logger {
	return slog.New(riverLogHandler{base: o.logger.With("component", "river").Handler()})
}

type riverLogHandler struct{ base slog.Handler }

func (h riverLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}
func (h riverLogHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.base.Handle(ctx, slog.NewRecord(record.Time, record.Level, "river runtime event", 0))
}
func (h riverLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h riverLogHandler) WithGroup(string) slog.Handler      { return h }
