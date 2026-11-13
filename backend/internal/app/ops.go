package app

import (
	"dicho/internal/observability"
	"dicho/internal/platform"
	"net/http"
)

func NewOpsHandler(db *platform.DB, limiter *platform.RateLimiter, observer *observability.Observer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		platform.Write(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", readiness(db, limiter))
	if observer != nil {
		mux.Handle("GET /metrics", observer.Metrics())
	}
	return mux
}

func readiness(db *platform.DB, limiter *platform.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil || db.Pool == nil || db.Pool.Pool == nil || db.Pool.Ping(r.Context()) != nil {
			platform.Fail(w, platform.Err(503, 5003, "Cơ sở dữ liệu chưa sẵn sàng"))
			return
		}
		state := "ok"
		if limiter != nil && limiter.Degraded() {
			state = "degraded"
		}
		platform.Write(w, 200, map[string]string{"status": state})
	}
}
