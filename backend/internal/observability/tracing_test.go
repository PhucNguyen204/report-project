package observability

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestTraceProviderExportsOTLPOnlyToExplicitEndpoint(t *testing.T) {
	requests := make(chan *collector.ExportTraceServiceRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var request collector.ExportTraceServiceRequest
		if r.URL.Path != "/v1/traces" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-protobuf" || proto.Unmarshal(raw, &request) != nil {
			w.WriteHeader(400)
			return
		}
		requests <- &request
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=PRIVATE-HEADER")
	provider, err := NewTraceProvider(ctx, server.URL+"/v1/traces")
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Shutdown(ctx)
	parent := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
	_, span := provider.Tracer("test").Start(parent, "safe span")
	span.End()
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		spans := request.ResourceSpans[0].ScopeSpans[0].Spans
		if len(spans) != 1 || spans[0].Name != "safe span" || !bytes.Equal(spans[0].TraceId, bytes.Repeat([]byte{0x11}, 16)) {
			t.Fatal("OTLP did not preserve span and trace")
		}
	case <-ctx.Done():
		t.Fatal("OTLP receiver did not receive span")
	}
}

func TestDisabledTraceExporterIgnoresEnvironmentDestination(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/v1/traces")
	provider, err := NewTraceProvider(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, span := provider.Tracer("test").Start(context.Background(), "disabled")
	span.End()
	if err = provider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = provider.Shutdown(context.Background()); err != nil || received.Load() != 0 {
		t.Fatal("disabled exporter used environment destination")
	}
	for _, endpoint := range []string{"PRIVATE-invalid", "http://PRIVATE-user:password@localhost/v1/traces", "http://localhost/v1/traces?PRIVATE"} {
		if _, err := NewTraceProvider(context.Background(), endpoint); err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("invalid exporter endpoint accepted or leaked")
		}
	}
}

func TestRiverLoggerDropsUntrustedMessagesAndAttributes(t *testing.T) {
	var logs bytes.Buffer
	o := New(Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	o.RiverLogger().With("PRIVATE-KEY", "PRIVATE-VALUE").WithGroup("PRIVATE-GROUP").Error("PRIVATE-MESSAGE", "error", "PRIVATE-ERROR")
	if strings.Contains(logs.String(), "PRIVATE") || !strings.Contains(logs.String(), `"component":"river"`) || !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatal("River error logs leaked opaque data or lost severity")
	}
}

func TestTraceCollectorFailureDoesNotRetryOrBlockShutdown(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(503)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := NewTraceProvider(ctx, server.URL+"/v1/traces")
	if err != nil {
		t.Fatal(err)
	}
	parent := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"})
	_, span := provider.Tracer("test").Start(parent, "failed collector")
	span.End()
	if provider.ForceFlush(ctx) == nil || received.Load() != 1 {
		t.Fatal("failed collector was retried or not reported")
	}
	stopCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := provider.Shutdown(stopCtx); err != nil {
		t.Fatalf("shutdown did not complete after failed batch: %v", err)
	}
}
