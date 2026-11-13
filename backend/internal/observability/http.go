package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func (o *Observer) HTTP(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		default:
			method = "OTHER"
		}
		_, route := mux.Handler(r)
		if _, path, ok := strings.Cut(route, " "); ok {
			route = path
		}
		if route == "" {
			route = "unmatched"
		}
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.MapCarrier{"traceparent": r.Header.Get("traceparent")})
		ctx, span := o.tracer.Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", route)))
		if id := traceID(ctx); id != "" {
			w.Header().Set("X-Trace-ID", id)
		}
		started := time.Now()
		response := &responseWriter{ResponseWriter: w}
		o.activeRequests.Inc()
		defer func() {
			panicked := recover()
			status := response.status
			if panicked != nil {
				status = 500
			} else if status == 0 {
				status = 200
			}
			duration := time.Since(started)
			labels := []string{method, route, strconv.Itoa(status)}
			o.requests.WithLabelValues(labels...).Inc()
			o.requestDuration.WithLabelValues(labels...).Observe(duration.Seconds())
			o.activeRequests.Dec()
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			if status >= 500 {
				span.SetStatus(codes.Error, "request failed")
			}
			o.logger.InfoContext(ctx, "http request", "request_id", w.Header().Get("X-Request-ID"), "trace_id", traceID(ctx), "method", method, "route", route, "status", status, "duration_ms", float64(duration)/float64(time.Millisecond))
			span.End()
			if panicked != nil {
				panic(panicked)
			}
		}()
		next.ServeHTTP(response, r.WithContext(ctx))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	if code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Flush() { _ = w.FlushError() }
