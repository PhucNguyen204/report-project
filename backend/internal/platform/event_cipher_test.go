package platform

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEventCipherProtectsPayloadAndAuthenticatesMetadata(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	cipher, err := NewEventCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	source := Event{Event: "email", Key: "email:one", Payload: json.RawMessage(`{"code":"123456","to":"owner@example.test"}`)}
	sealed, err := cipher.Seal(source)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(sealed)
	if bytes.Contains(wire, []byte("123456")) || bytes.Contains(wire, []byte("owner@example.test")) {
		t.Fatal("secret persisted in plaintext")
	}
	got, err := cipher.Open(sealed)
	if err != nil || !bytes.Equal(got.Payload, source.Payload) {
		t.Fatalf("roundtrip %v", err)
	}
	second, _ := cipher.Seal(source)
	if bytes.Equal(second.Payload, sealed.Payload) {
		t.Fatal("nonce reused")
	}
	for _, mutate := range []func(*Event){func(e *Event) { e.Key = "email:another" }, func(e *Event) { e.Event = "account.purge" }, func(e *Event) { e.Payload = json.RawMessage(`"dGFtcGVyZWQ="`) }, func(e *Event) { e.Encrypted = false }} {
		bad := sealed
		mutate(&bad)
		if _, err = cipher.Open(bad); err == nil {
			t.Fatal("tampered event accepted")
		}
	}
	other, _ := NewEventCipher(bytes.Repeat([]byte{8}, 32))
	if _, err = other.Open(sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = NewEventCipher([]byte("short")); err == nil {
		t.Fatal("invalid key accepted")
	}
}
