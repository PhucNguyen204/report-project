package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func observerFixture(t *testing.T) (*Observer, *bytes.Buffer, *tracetest.InMemoryExporter) {
	t.Helper()
	var logs bytes.Buffer
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	o := New(Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), TracerProvider: provider})
	return o, &logs, exporter
}

func sample(t *testing.T, o *Observer, name string, labels map[string]string, histogram bool) float64 {
	t.Helper()
	families, err := o.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != len(labels) {
				continue
			}
			match := true
			for _, label := range metric.Label {
				if labels[label.GetName()] != label.GetValue() {
					match = false
				}
			}
			if match {
				if histogram {
					return float64(metric.GetHistogram().GetSampleCount())
				}
				if metric.Counter != nil {
					return metric.GetCounter().GetValue()
				}
				return metric.GetGauge().GetValue()
			}
		}
	}
	return 0
}

func TestHTTPUsesRegisteredRoutesAndSanitizedCorrelatedLogs(t *testing.T) {
	o, logs, exporter := observerFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /objects/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "request-1")
		w.WriteHeader(http.StatusNoContent)
	})
	handler := o.HTTP(mux, mux)
	for _, id := range []string{"PRIVATE-ID-A", "PRIVATE-ID-B"} {
		r := httptest.NewRequest("GET", "/objects/"+id+"?token=PRIVATE-TOKEN", strings.NewReader("PRIVATE-BODY"))
		r.Header.Set("Authorization", "Bearer PRIVATE-AUTH")
		r.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
		r.Header.Set("tracestate", "vendor=PRIVATE-STATE")
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	labels := map[string]string{"method": "GET", "route": "/objects/{id}", "status": "204"}
	if got := sample(t, o, "dicho_http_requests_total", labels, false); got != 2 {
		t.Fatalf("route count=%v", got)
	}
	if got := sample(t, o, "dicho_http_request_duration_seconds", labels, true); got != 2 {
		t.Fatalf("duration observations=%v", got)
	}
	if got := sample(t, o, "dicho_http_requests_active", nil, false); got != 0 {
		t.Fatalf("active=%v", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["request_id"] != "request-1" || entry["trace_id"] != "11111111111111111111111111111111" || entry["route"] != "/objects/{id}" || entry["status"] != float64(204) {
			t.Fatalf("uncorrelated request: %v", entry)
		}
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	for _, span := range spans {
		if span.Name != "GET /objects/{id}" || span.SpanKind != trace.SpanKindServer || span.Parent.SpanID().String() != "2222222222222222" {
			t.Fatalf("wrong server span: %+v", span)
		}
		if strings.Contains(string(mustJSON(t, span.Attributes)), "PRIVATE") {
			t.Fatal("private input in span attributes")
		}
		if span.SpanContext.TraceState().String() != "" {
			t.Fatal("untrusted external tracestate propagated")
		}
	}
	response := httptest.NewRecorder()
	o.Metrics().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "dicho_http_requests_total") || strings.Contains(response.Body.String()+logs.String(), "PRIVATE") {
		t.Fatal("metrics/logs contain private input or cannot be scraped")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHTTPRecordsFinalStatusAndBoundsUnknownMethods(t *testing.T) {
	o, _, exporter := observerFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /write", func(http.ResponseWriter, *http.Request) {})
	handler := o.HTTP(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/write" {
			w.WriteHeader(103)
			w.WriteHeader(502)
			w.WriteHeader(201)
			_, _ = w.Write([]byte("error"))
		} else {
			_, _ = w.Write([]byte("ok"))
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/write", nil))
	for _, method := range []string{"PRIVATE-METHOD-1", "PRIVATE-METHOD-2"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/PRIVATE-PATH", nil))
	}
	if got := sample(t, o, "dicho_http_requests_total", map[string]string{"method": "POST", "route": "/write", "status": "502"}, false); got != 1 {
		t.Fatalf("final status count=%v", got)
	}
	if got := sample(t, o, "dicho_http_requests_total", map[string]string{"method": "OTHER", "route": "unmatched", "status": "200"}, false); got != 2 {
		t.Fatalf("bounded method count=%v", got)
	}
	if exporter.GetSpans()[0].Status.Code != codes.Error {
		t.Fatal("5xx span was not an error")
	}
}

func TestWorkerRecordsAttemptsWithoutPayloadOrRawError(t *testing.T) {
	o, logs, exporter := observerFixture(t)
	ctx, parent := o.tracer.Start(context.Background(), "parent")
	sentinel := errors.New("PRIVATE-ERROR")
	if err := o.Work(ctx, "email", 1, func(ctx context.Context) error {
		if trace.SpanContextFromContext(ctx).TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("worker lost trace")
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("worker changed error: %v", err)
	}
	if err := o.Work(ctx, "email", 2, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() != "PRIVATE-PANIC" {
				t.Fatal("worker swallowed panic")
			}
		}()
		_ = o.Work(ctx, "PRIVATE-KIND", 1, func(context.Context) error { panic("PRIVATE-PANIC") })
	}()
	parent.End()
	for _, item := range []struct{ kind, result string }{{"email", "error"}, {"email", "ok"}, {"other", "panic"}} {
		labels := map[string]string{"kind": item.kind, "result": item.result}
		if sample(t, o, "dicho_worker_attempts_total", labels, false) != 1 || sample(t, o, "dicho_worker_attempt_duration_seconds", labels, true) != 1 {
			t.Fatalf("missing worker observation: %v", labels)
		}
	}
	if sample(t, o, "dicho_worker_attempts_active", nil, false) != 0 || strings.Contains(logs.String(), "PRIVATE") {
		t.Fatal("worker leaked private data or active gauge")
	}
	spans := exporter.GetSpans()
	if len(spans) != 4 || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() || spans[0].SpanContext.SpanID() == spans[1].SpanContext.SpanID() || spans[0].Status.Code != codes.Error {
		t.Fatal("attempt spans are missing or not distinct children")
	}
}

func TestConcurrentRequestsAndScrapesReleaseActiveCounts(t *testing.T) {
	o := New(Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), PoolStats: func() (int64, int64, int64) { return 4, 2, 10 }})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /objects/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	handler := o.HTTP(mux, mux)
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() { handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/objects/1", nil)) })
	}
	for range 10 {
		wg.Go(func() { o.Metrics().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/metrics", nil)) })
	}
	wg.Wait()
	if sample(t, o, "dicho_http_requests_total", map[string]string{"method": "GET", "route": "/objects/{id}", "status": "204"}, false) != 40 || sample(t, o, "dicho_http_requests_active", nil, false) != 0 {
		t.Fatal("concurrent request metrics lost or leaked")
	}
	for name, want := range map[string]float64{"dicho_db_connections": 4, "dicho_db_connections_acquired": 2, "dicho_db_connections_max": 10} {
		if sample(t, o, name, nil, false) != want {
			t.Fatalf("%s incorrect", name)
		}
	}
}

func TestHTTPFlushKeepsActualStatusAndPanicReleasesGauge(t *testing.T) {
	o, _, _ := observerFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flush", func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("PRIVATE-PANIC") })
	handler := o.HTTP(mux, mux)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/flush", nil))
	if !w.Flushed || w.Code != 200 || sample(t, o, "dicho_http_requests_total", map[string]string{"method": "GET", "route": "/flush", "status": "200"}, false) != 1 {
		t.Fatal("flush changed response or recorded a later unsent status")
	}
	func() {
		defer func() {
			if recover() != "PRIVATE-PANIC" {
				t.Fatal("HTTP panic swallowed")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/panic", nil))
	}()
	if sample(t, o, "dicho_http_requests_active", nil, false) != 0 || sample(t, o, "dicho_http_requests_total", map[string]string{"method": "GET", "route": "/panic", "status": "500"}, false) != 1 {
		t.Fatal("HTTP panic not recorded or active request leaked")
	}
}
