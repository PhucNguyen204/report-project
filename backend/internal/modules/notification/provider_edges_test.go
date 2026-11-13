package notification

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Token deletion is valid only for the structured FCM UNREGISTERED error, never for diagnostic text or malformed success.
func TestFCMRejectsMalformedSuccessAndDiagnosticTokenText(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{200, `{"debug":"UNREGISTERED"}`}, {200, ``}, {404, `{"error":{"message":"UNREGISTERED","status":"NOT_FOUND"}}`}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		f := FCM{Project: "test", Client: server.Client(), BaseURL: server.URL}
		err := f.Send(context.Background(), "token", "Title", "Body")
		server.Close()
		if err == nil || errors.Is(err, ErrInvalidDevice) {
			t.Fatalf("unsafe classification status%d: %v", tc.status, err)
		}
	}
}

// Configuration/input/network failures must remain retryable or typed400 and never be confused with an invalid device token.
func TestFCMInputStatusAndCancellationContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("{"))
	}))
	defer server.Close()
	f := FCM{Project: "project", Client: server.Client(), BaseURL: server.URL}
	if err := f.Send(context.Background(), "token", "Title", "Body"); err == nil || errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("truncated HTTP body %v", err)
	}
	for _, tc := range []struct {
		sender  FCM
		message PushMessage
		status  int
	}{{FCM{}, PushMessage{Token: "token", Title: "Title"}, 503}, {f, PushMessage{Title: "Title"}, 400}, {f, PushMessage{Token: "token"}, 400}, {f, PushMessage{Token: "token", Title: "Title", Body: strings.Repeat("x", 5000)}, 400}, {FCM{Project: "project", Client: server.Client(), BaseURL: "://private-server-secret"}, PushMessage{Token: "token", Title: "Title"}, 503}} {
		err := tc.sender.SendMessage(context.Background(), tc.message)
		var app *platform.Error
		if !errors.As(err, &app) || app.Status != tc.status || strings.Contains(err.Error(), "private-server-secret") {
			t.Fatalf("FCM edge %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Send(ctx, "token", "Title", "Body"); !errors.Is(err, context.Canceled) {
		t.Fatalf("FCM cancellation %v", err)
	}
	for _, status := range []int{401, 403, 429, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"private-server-secret"}}`))
		}))
		err := (FCM{Project: "project", Client: srv.Client(), BaseURL: srv.URL}).Send(context.Background(), "token", "Title", "Body")
		srv.Close()
		var app *platform.Error
		if !errors.As(err, &app) || app.Status != 503 || errors.Is(err, ErrInvalidDevice) || strings.Contains(err.Error(), "private-server-secret") {
			t.Fatalf("FCM HTTP%d %v", status, err)
		}
	}
}

// A canceled SMTP context must interrupt the server greeting wait instead of blocking until the fixed network deadline.
func TestSMTPContextCancellationInterruptsGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (SMTP{Addr: listener.Addr().String(), From: "sender@test.local"}).Send(ctx, "recipient@test.local", "Subject", "Body")
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("SMTP did not connect")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP ignored context cancellation")
	}
}
