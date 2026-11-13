package observability

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type Options struct {
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	PoolStats      func() (total, acquired, maximum int64)
}

type Observer struct {
	registry        *prometheus.Registry
	logger          *slog.Logger
	tracer          trace.Tracer
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	activeRequests  prometheus.Gauge
	attempts        *prometheus.CounterVec
	attemptDuration *prometheus.HistogramVec
	activeAttempts  prometheus.Gauge
}

func New(opts Options) *Observer {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.TracerProvider == nil {
		opts.TracerProvider = noop.NewTracerProvider()
	}
	buckets := []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10, 20, 30}
	o := &Observer{
		registry: prometheus.NewRegistry(), logger: opts.Logger, tracer: opts.TracerProvider.Tracer("dicho"),
		requests:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dicho_http_requests_total", Help: "Completed API requests."}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "dicho_http_request_duration_seconds", Help: "API response duration.", Buckets: buckets}, []string{"method", "route", "status"}),
		activeRequests:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "dicho_http_requests_active", Help: "API requests currently executing."}),
		attempts:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dicho_worker_attempts_total", Help: "Completed River job attempts."}, []string{"kind", "result"}),
		attemptDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "dicho_worker_attempt_duration_seconds", Help: "River job attempt duration.", Buckets: buckets}, []string{"kind", "result"}),
		activeAttempts:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "dicho_worker_attempts_active", Help: "River job attempts currently executing."}),
	}
	o.registry.MustRegister(o.requests, o.requestDuration, o.activeRequests, o.attempts, o.attemptDuration, o.activeAttempts, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	if opts.PoolStats != nil {
		for name, read := range map[string]func() float64{
			"dicho_db_connections":          func() float64 { n, _, _ := opts.PoolStats(); return float64(n) },
			"dicho_db_connections_acquired": func() float64 { _, n, _ := opts.PoolStats(); return float64(n) },
			"dicho_db_connections_max":      func() float64 { _, _, n := opts.PoolStats(); return float64(n) },
		} {
			o.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: "PostgreSQL connection pool size."}, read))
		}
	}
	return o
}

func (o *Observer) Metrics() http.Handler {
	return promhttp.HandlerFor(o.registry, promhttp.HandlerOpts{MaxRequestsInFlight: 4})
}

func traceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
