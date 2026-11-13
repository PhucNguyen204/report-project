//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Malformed durable events must fail as client errors without echoing tokens, addresses, or payload fragments into worker errors.
func TestMalformedEventsAreTypedAndRedacted(t *testing.T) {
	s := New(testutil.NewDB(t), nil, nil)
	for _, event := range []platform.Event{{Event: "secret-credential"}, {Event: "notification", Payload: json.RawMessage(`{"userId":"secret-token"`)}, {Event: "notification", Payload: json.RawMessage(`{"userId":"secret-token","title":"Title"}`)}, {Event: "notification", Payload: json.RawMessage(`{"userId":"` + platform.ID() + `","title":"Title","type":"secret-token"}`)}, {Event: "notification", Payload: json.RawMessage(`{"userId":"` + platform.ID() + `","title":"Title","spaceId":"secret-token"}`)}, {Event: "notification", Payload: json.RawMessage(`{"userId":"` + platform.ID() + `","title":"Title","data":{"from":"secret-token"}}`)}, {Event: "email", Payload: json.RawMessage(`{`)}, {Event: "email", Payload: json.RawMessage(`{"to":"secret-token","body":"private-code"}`)}, {Event: "email", Payload: json.RawMessage(`{"to":"to@test.local"}`)}, {Event: "email", Payload: json.RawMessage(`{"to":"to@test.local","code":"short"}`)}, {Event: "email", Payload: json.RawMessage(`{"to":"to@test.local","code":"secret"}`)}, {Event: "email", Payload: json.RawMessage(`{"to":"to@test.local","subject":"hello\r\nBcc:secret-token","body":"private-code"}`)}} {
		event.Key = "validation"
		err := s.Handle(context.Background(), event)
		var app *platform.Error
		if !errors.As(err, &app) || app.Status != 400 {
			t.Fatalf("malformed event not typed400: %v", err)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-code") {
			t.Fatal("private event leaked")
		}
	}
}

// A valid email with unavailable infrastructure is retryable rather than a successful delivery or malformed-user error.
func TestMissingEmailProviderIsRetryable(t *testing.T) {
	err := New(nil, nil, nil).Handle(context.Background(), platform.Event{Event: "email", Payload: json.RawMessage(`{"to":"to@test.local","subject":"Subject","body":"Private body"}`)})
	var app *platform.Error
	if !errors.As(err, &app) || app.Status != 503 {
		t.Fatalf("missing email provider %v", err)
	}
}
