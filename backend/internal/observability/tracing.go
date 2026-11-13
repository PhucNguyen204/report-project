package observability

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func ValidateTraceEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.Path != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.HasSuffix(u.Host, ":") {
		if u.Port() == "" {
			return nil
		}
		if port, err := strconv.Atoi(u.Port()); err == nil && port > 0 && port <= 65535 {
			return nil
		}
	}
	return errors.New("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must be an absolute HTTP(S) URL with a path and without credentials, query or fragment")
}

func NewTraceProvider(ctx context.Context, endpoint string) (*sdktrace.TracerProvider, error) {
	if err := ValidateTraceEndpoint(endpoint); err != nil {
		return nil, err
	}
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "dicho-backend"))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(.1))),
	}
	if endpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint), otlptracehttp.WithHeaders(map[string]string{}), otlptracehttp.WithCompression(otlptracehttp.NoCompression), otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}), otlptracehttp.WithTimeout(2*time.Second), otlptracehttp.WithHTTPClient(&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
		if err != nil {
			return nil, errors.New("cannot initialize telemetry exporter")
		}
		opts = append(opts, sdktrace.WithBatcher(exporter, sdktrace.WithMaxQueueSize(1024), sdktrace.WithMaxExportBatchSize(128), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(2*time.Second)))
	}
	return sdktrace.NewTracerProvider(opts...), nil
}
