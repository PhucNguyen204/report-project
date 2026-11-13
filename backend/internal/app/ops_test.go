package app

import (
	"bytes"
	"context"
	"dicho/internal/observability"
	"dicho/internal/platform"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestOperationalConfigurationIsExplicitAndDoesNotLeakInvalidInput(t *testing.T) {
	env := developmentEnv()
	cfg, err := LoadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil || cfg.OpsListenAddr != "127.0.0.1:8081" || cfg.TraceEndpoint != "" {
		t.Fatalf("ops defaults incorrect: err=%v", err)
	}
	for key, values := range map[string][]string{
		"OPS_LISTEN_ADDR":                    {"PRIVATE-invalid", "127.0.0.1:99999", "127.0.0.1:abc", "127.0.0.1:0"},
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": {"PRIVATE-invalid", "ftp://PRIVATE-host/v1/traces", "http://PRIVATE-user:password@localhost/v1/traces", "http://localhost/v1/traces?token=PRIVATE", "http://localhost/v1/traces#PRIVATE"},
	} {
		for _, value := range values {
			env[key] = value
			_, err := LoadRuntimeConfig(func(key string) string { return env[key] })
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "password") {
				t.Fatalf("invalid %s accepted or leaked", key)
			}
		}
		delete(env, key)
	}
	env["OPS_LISTEN_ADDR"], env["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] = "0.0.0.0:8081", "http://collector:4318/v1/traces"
	if _, err := LoadRuntimeConfig(func(key string) string { return env[key] }); err != nil {
		t.Fatal(err)
	}
}

func TestOpsMetricsAreSeparateFromPublicAPIAndGatewayFailuresAreObserved(t *testing.T) {
	var logs bytes.Buffer
	provider := sdktrace.NewTracerProvider()
	defer provider.Shutdown(context.Background())
	o := observability.New(observability.Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), TracerProvider: provider})
	mux := http.NewServeMux()
	handler := NewHandler(nil, func(context.Context, string) (platform.Principal, error) {
		return platform.Principal{UserID: "user"}, nil
	}, Config{Mux: mux, Observer: o}, func(m *http.ServeMux) {
		m.HandleFunc("GET /it4788/v1/objects/{id}", func(http.ResponseWriter, *http.Request) { panic("PRIVATE-PANIC") })
	})
	for _, tc := range []struct {
		path, version, token string
		status               int
	}{
		{"/it4788/v1/objects/PRIVATE-ID?token=PRIVATE", "", "", 400},
		{"/it4788/v1/objects/PRIVATE-ID", "1.0.0", "", 401},
		{"/it4788/v1/objects/PRIVATE-ID", "1.0.0", "PRIVATE", 500},
		{"/metrics", "1.0.0", "PRIVATE", 404},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("X-App-Version", tc.version)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("gateway status=%d want=%d", w.Code, tc.status)
		}
		if tc.status != 404 {
			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(w.Header().Get("X-Trace-ID")) != 32 || response["traceId"] != w.Header().Get("X-Trace-ID") {
				t.Fatal("gateway error lacks usable trace correlation")
			}
		}
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 4 || strings.Contains(logs.String(), "PRIVATE") {
		t.Fatal("missing gateway logs or private data leaked")
	}
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["request_id"] == "" || entry["trace_id"] == "" || (entry["route"] != "/it4788/v1/objects/{id}" && entry["route"] != "unmatched") {
			t.Fatalf("uncorrelated gateway log: %v", entry)
		}
	}
	ops := NewOpsHandler(nil, nil, o)
	for _, tc := range []struct {
		path string
		code int
	}{{"/healthz", 200}, {"/readyz", 503}, {"/metrics", 200}, {"/it4788/v1/objects/1", 404}} {
		w := httptest.NewRecorder()
		ops.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("ops %s status=%d", tc.path, w.Code)
		}
		if tc.path == "/metrics" && (!strings.Contains(w.Body.String(), `route="/it4788/v1/objects/{id}",status="500"`) || strings.Contains(w.Body.String(), "PRIVATE")) {
			t.Fatal("ops metrics missing gateway failure or exposing private input")
		}
	}
	w := httptest.NewRecorder()
	NewOpsHandler(nil, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 404 {
		t.Fatal("unconfigured observer exposed metrics")
	}
}
