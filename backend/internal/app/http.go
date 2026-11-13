package app

import (
	"bytes"
	"crypto/sha256"
	"dicho/api"
	"dicho/internal/platform"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Mux               *http.ServeMux
	MinimumAppVersion string
	Limiter           *platform.RateLimiter
	TrustedProxies    []net.IPNet
}

func NewHandler(db *platform.DB, auth platform.AuthenticateFunc, cfg Config, register ...func(*http.ServeMux)) http.Handler {
	mux := cfg.Mux
	if mux == nil {
		mux = http.NewServeMux()
	}
	for _, fn := range register {
		fn(mux)
	}
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(api.Document)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		platform.Write(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if db == nil || db.Pool == nil || db.Pool.Ping(r.Context()) != nil {
			platform.Fail(w, platform.Err(503, 5003, "Cơ sở dữ liệu chưa sẵn sàng"))
			return
		}
		state := "ok"
		if cfg.Limiter != nil && cfg.Limiter.Degraded() {
			state = "degraded"
		}
		platform.Write(w, 200, map[string]string{"status": state})
	})
	if cfg.MinimumAppVersion == "" {
		cfg.MinimumAppVersion = "1.0.0"
	}
	if cfg.Limiter == nil {
		cfg.Limiter = platform.NewRateLimiter(nil, platform.RealClock{})
	}
	protected := platform.Auth(auth, scopeGuard(rate(cfg, platform.Idempotency(db, mux))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", platform.ID())
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if recover() != nil {
				platform.Fail(w, platform.Err(500, 5000, "Lỗi hệ thống"))
			}
		}()
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/openapi.json" {
			mux.ServeHTTP(w, r)
			return
		}
		version := r.Header.Get("X-App-Version")
		comparison, err := compareVersion(version, cfg.MinimumAppVersion)
		if err != nil {
			platform.Fail(w, platform.Invalid("X-App-Version phải có dạng x.y.z"))
			return
		}
		if comparison < 0 {
			platform.Fail(w, platform.Err(426, 4260, "Cần cập nhật ứng dụng"))
			return
		}
		if r.Body != nil {
			b, err := io.ReadAll(io.LimitReader(r.Body, 1_048_577))
			if err != nil || len(b) > 1_048_576 {
				platform.Fail(w, platform.Err(413, 2002, "Dữ liệu quá lớn"))
				return
			}
			if len(b) > 0 {
				content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || content != "application/json" {
					platform.Fail(w, platform.Err(415, 2002, "Content-Type phải là application/json"))
					return
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		protected.ServeHTTP(w, r)
	})
}
func compareVersion(a, b string) (int, error) {
	parse := func(s string) ([3]int, error) {
		var v [3]int
		p := strings.Split(s, ".")
		if len(p) != 3 {
			return v, fmt.Errorf("invalid version")
		}
		for i, n := range p {
			if n == "" || strings.HasPrefix(n, "+") || strings.HasPrefix(n, "-") {
				return v, fmt.Errorf("invalid version")
			}
			k, err := strconv.Atoi(n)
			if err != nil || k < 0 {
				return v, fmt.Errorf("invalid version")
			}
			v[i] = k
		}
		return v, nil
	}
	x, err := parse(a)
	if err != nil {
		return 0, err
	}
	y, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, nil
		}
		if x[i] > y[i] {
			return 1, nil
		}
	}
	return 0, nil
}
func scopeGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if platform.Current(r).SystemRole == "admin" && (r.URL.Path == "/it4788/v1/spaces" || strings.HasPrefix(r.URL.Path, "/it4788/v1/spaces/")) {
			platform.Fail(w, platform.NotFound())
			return
		}
		next.ServeHTTP(w, r)
	})
}
func rate(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := platform.Current(r).UserID
		host := clientIP(r, cfg.TrustedProxies)
		key := ""
		limit := 0
		period := time.Minute
		switch r.URL.Path {
		case "/it4788/v1/auth/login":
			key = "login:" + host
			limit = 10
		case "/it4788/v1/auth/register":
			key = "register:" + host
			limit = 5
			period = time.Hour
		}
		check := func(key string, n int, period time.Duration) bool {
			sum := sha256.Sum256([]byte(key))
			retry, err := cfg.Limiter.Check(r.Context(), fmt.Sprintf("%x", sum), n, period)
			if err != nil {
				platform.Fail(w, platform.Err(503, 5003, "Bộ giới hạn chưa sẵn sàng"))
				return false
			}
			if retry > 0 {
				seconds := int64((retry + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
				platform.Fail(w, platform.Err(429, 4290, "Vượt giới hạn yêu cầu"))
				return false
			}
			return true
		}
		if limit > 0 && !check(key, limit, period) {
			return
		}
		if user != "" {
			if !check("api:"+user, 120, time.Minute) {
				return
			}
			if strings.HasSuffix(r.URL.Path, "/foods/suggest") && !check("suggest:"+user, 20, time.Second) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request, networks []net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	trusted := func(ip net.IP) bool {
		for _, network := range networks {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	forwarded := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(forwarded) - 1; i >= 0 && trusted(ip); i-- {
		candidate := net.ParseIP(strings.TrimSpace(forwarded[i]))
		if candidate == nil {
			return host
		}
		ip = candidate
	}
	return ip.String()
}
