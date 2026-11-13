package app

import (
	"context"
	"dicho/internal/platform"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gateway(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(nil, func(_ context.Context, token string) (platform.Principal, error) {
		if token != "valid" {
			return platform.Principal{}, platform.Err(401, 3001, "invalid")
		}
		return platform.Principal{UserID: "user"}, nil
	}, Config{MinimumAppVersion: "1.0.0"}, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /it4788/v1/me", func(w http.ResponseWriter, r *http.Request) { platform.Write(w, 200, platform.Current(r).UserID) })
		mux.HandleFunc("POST /it4788/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
			var v map[string]any
			if err := platform.Decode(r, &v); err != nil {
				platform.Fail(w, err)
				return
			}
			platform.Write(w, 200, v)
		})
	})
}

func TestTrustedProxyChainCannotUseSpoofedPrefixToBypassLoginQuota(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	h := rate(Config{Limiter: platform.NewRateLimiter(nil, nil), TrustedProxies: []net.IPNet{*network}}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for i := range 11 {
		r := httptest.NewRequest("POST", "/it4788/v1/auth/login", nil)
		r.RemoteAddr = "10.0.0.5:443"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d, 192.0.2.1, 10.0.0.3", i+1))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (i < 10 && w.Code != 200) || (i == 10 && w.Code != 429) {
			t.Fatalf("request%d status%d", i+1, w.Code)
		}
	}
}

func TestGatewayEnforcesVersionAuthAndBodyPolicy(t *testing.T) {
	h := gateway(t)
	for _, tc := range []struct {
		method, path, version, token, body, mime string
		status                                   int
	}{
		{"GET", "/healthz", "", "", "", "", 200},
		{"GET", "/readyz", "", "", "", "", 503},
		{"GET", "/it4788/v1/me", "", "valid", "", "", 400},
		{"GET", "/it4788/v1/me", "0.9.0", "valid", "", "", 426},
		{"GET", "/it4788/v1/me", "1.0.0", "", "", "", 401},
		{"GET", "/it4788/v1/me", "1.0.0", "valid", "", "", 200},
		{"POST", "/it4788/v1/auth/login", "1.0.0", "", "{}", "text/plain", 415},
		{"POST", "/it4788/v1/auth/login", "1.0.0", "", strings.Repeat(" ", 1_048_577), "application/json", 413},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-App-Version", tc.version)
		r.Header.Set("Content-Type", tc.mime)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s got%d want%d %s", tc.method, tc.path, w.Code, tc.status, w.Body)
		}
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
	}
}

func TestGatewayDoesNotTrustSpoofedForwardedIPForLoginLimit(t *testing.T) {
	h := gateway(t)
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest("POST", "/it4788/v1/auth/login", strings.NewReader(`{}`))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-App-Version", "1.0.0")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i < 10 && w.Code != 200 {
			t.Fatalf("early limit%d", w.Code)
		}
		if i == 10 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatalf("missing throttle%d %s", w.Code, w.Body)
		}
	}
}

func TestAdministratorCannotReachSpaceHandlersThroughGateway(t *testing.T) {
	calls := 0
	h := NewHandler(nil, func(context.Context, string) (platform.Principal, error) {
		return platform.Principal{UserID: "admin", SystemRole: "admin"}, nil
	}, Config{}, func(m *http.ServeMux) {
		for _, path := range []string{"/it4788/v1/spaces", "/it4788/v1/spaces/{sid}", "/it4788/v1/spaces/{sid}/lots", "/it4788/v1/me"} {
			m.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
				calls++
				platform.Write(w, 200, map[string]bool{"reached": true})
			})
		}
	})
	for _, path := range []string{"/it4788/v1/spaces", "/it4788/v1/spaces/household", "/it4788/v1/spaces/household/lots"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-App-Version", "1.0.0")
		r.Header.Set("Authorization", "Bearer captured")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 404 || calls != 0 {
			t.Fatalf("administrator reached family route%s status%d calls%d", path, w.Code, calls)
		}
	}
	r := httptest.NewRequest("GET", "/it4788/v1/me", nil)
	r.Header.Set("X-App-Version", "1.0.0")
	r.Header.Set("Authorization", "Bearer captured")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 {
		t.Fatalf("administrator own profile blocked status%d calls%d", w.Code, calls)
	}
}

func TestCanceledLimiterRequestDoesNotReachHandler(t *testing.T) {
	called := false
	h := rate(Config{Limiter: platform.NewRateLimiter(nil, nil)}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	ctx, cancel := context.WithCancel(platform.WithPrincipal(context.Background(), platform.Principal{UserID: "user"}))
	cancel()
	r := httptest.NewRequest("GET", "/it4788/v1/me", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || called {
		t.Fatalf("failed limiter bypassed status%d reached%v", w.Code, called)
	}
}
