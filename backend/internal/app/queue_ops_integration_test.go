//go:build integration

package app

import (
	"bytes"
	"context"
	"dicho/internal/observability"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrivateOpsReportsRealQueueAndCollectionFailure(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	if _, err := platform.ConfigureQueue(ctx, db, nil, bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return db.Enqueue(ctx, tx, "email", platform.ID(), map[string]string{"to": "PRIVATE@example.test", "code": "PRIVATE-OTP"}, db.Clock.Now().Add(-time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	o := observability.New(observability.Options{QueueStats: db.QueueStats})
	h := NewOpsHandler(db, nil, o)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "dicho_queue_stats_up 1") || !strings.Contains(w.Body.String(), `dicho_queue_jobs{kind="email",state="available"} 1`) || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("private ops lost the queue snapshot or exposed an encrypted payload")
	}
	db.Pool.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "dicho_queue_stats_up 0") || strings.Contains(w.Body.String(), "dicho_queue_jobs{") || strings.Contains(w.Body.String(), "dicho_queue_oldest_due_seconds{") {
		t.Fatal("failed queue source was published as healthy or stale metrics")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal("queue telemetry failure changed liveness")
	}
}
