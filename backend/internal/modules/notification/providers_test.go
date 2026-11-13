package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFCMSendsOnlyExpectedPayloadAndClassifiesRetry(t *testing.T) {
	status := 200
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/test/messages:send" || r.Method != "POST" {
			t.Error(r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(status)
		if status == 404 {
			_, _ = w.Write([]byte(`{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`))
		} else if status == 200 {
			_, _ = w.Write([]byte(`{"name":"projects/test/messages/one"}`))
		}
	}))
	defer srv.Close()
	f := FCM{Project: "test", Client: srv.Client(), BaseURL: srv.URL}
	if err := f.Send(context.Background(), "token", "Title", "Body"); err != nil {
		t.Fatal(err)
	}
	if received["message"].(map[string]any)["token"] != "token" {
		t.Fatal(received)
	}
	status = 503
	if err := f.Send(context.Background(), "token", "Title", "Body"); err == nil || errors.Is(err, ErrInvalidDevice) {
		t.Fatal("transient failure not retryable")
	}
	status = 404
	if err := f.Send(context.Background(), "token", "Title", "Body"); !errors.Is(err, ErrInvalidDevice) {
		t.Fatal("unregistered token not classified")
	}
}

func TestSMTPRejectsHeaderInjectionBeforeConnecting(t *testing.T) {
	s := SMTP{Addr: "127.0.0.1:1", From: "sender@example.test"}
	if err := s.Send(context.Background(), "to@example.test", "Title\r\nBcc: evil@example.test", "body"); err == nil {
		t.Fatal("header injection accepted")
	}
}

func TestFCMDoesNotDeleteDeviceForUnknownProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","message":"Project missing"}}`))
	}))
	defer srv.Close()
	f := FCM{Project: "missing", Client: srv.Client(), BaseURL: srv.URL}
	if err := f.Send(context.Background(), "token", "title", "body"); err == nil || errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("configuration failure classified as invalid device: %v", err)
	}
}
