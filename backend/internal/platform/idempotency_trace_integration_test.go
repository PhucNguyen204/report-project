//go:build integration

package platform_test

import (
	"context"
	"dicho/internal/app"
	"dicho/internal/observability"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestCommandErrorsAndReplaysUseCurrentTraceWithoutCachingCorrelation(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	user := platform.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Trace Member','unused','active')`, user, user+"@example.test"); err != nil {
		t.Fatal(err)
	}
	provider := sdktrace.NewTracerProvider()
	defer provider.Shutdown(ctx)
	o := observability.New(observability.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), TracerProvider: provider})
	handler := app.NewHandler(db, func(context.Context, string) (platform.Principal, error) {
		return platform.Principal{UserID: user}, nil
	}, app.Config{Observer: o}, func(m *http.ServeMux) {
		m.HandleFunc("POST /it4788/v1/command", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
			var in struct {
				Fail bool `json:"fail"`
			}
			if err := platform.Decode(r, &in); err != nil {
				return err
			}
			if in.Fail {
				return platform.Invalid("invalid command")
			}
			platform.Write(w, 201, map[string]string{"id": "created-once"})
			return nil
		}))
	})
	call := func(body, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/it4788/v1/command", strings.NewReader(body))
		r.Header.Set("X-App-Version", "1.0.0")
		r.Header.Set("Authorization", "Bearer fixture")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	failed := call(`{"fail":true}`, platform.ID())
	var errorBody map[string]any
	if err := json.Unmarshal(failed.Body.Bytes(), &errorBody); err != nil {
		t.Fatal(err)
	}
	if failed.Code != 400 || len(failed.Header().Get("X-Trace-ID")) != 32 || errorBody["traceId"] != failed.Header().Get("X-Trace-ID") {
		t.Fatal("transactional command error lost current trace")
	}
	key := platform.ID()
	first, replay := call(`{}`, key), call(`{}`, key)
	if first.Code != 201 || replay.Code != 201 || replay.Header().Get("Idempotent-Replayed") != "true" || first.Body.String() != replay.Body.String() {
		t.Fatal("command replay behavior changed")
	}
	if len(first.Header().Values("X-Trace-ID")) != 1 || len(replay.Header().Values("X-Trace-ID")) != 1 || first.Header().Get("X-Trace-ID") == replay.Header().Get("X-Trace-ID") {
		t.Fatal("trace header duplicated or reused from cache")
	}
	var headers string
	if err := db.Pool.QueryRow(ctx, "SELECT headers::text FROM platform.idempotency_keys WHERE user_id=$1 AND key=$2", user, key).Scan(&headers); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(headers), "trace") || strings.Contains(strings.ToLower(headers), "request-id") {
		t.Fatal("correlation metadata cached with business response")
	}
}
