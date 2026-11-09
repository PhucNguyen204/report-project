package accounts

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestValidateRegistrationRejectsInvalidIdentity(t *testing.T) {
	for _, tt := range []struct{ name, email, password string }{
		{"A", "a@example.com", "ValidPass1"},
		{"An", "invalid", "ValidPass1"},
		{"An", "a@example.com", "short1A"},
		{"An", "a@example.com", "onlylowercase1"},
		{"An", "a@example.com", "ONLYUPPERCASE1"},
		{"An", "a@example.com", "NoDigitsHere"},
		{"An", "a@example.com", strings.Repeat("A", 33) + "a1"},
	} {
		if validateRegistration(tt.name, tt.email, tt.password) == nil {
			t.Errorf("accepted invalid registration %+v", tt)
		}
	}
	if err := validateRegistration("Nguyễn An", "An@Example.com", "ValidPass1"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordHashVerifiesOnlyOriginalAndRejectsMalformed(t *testing.T) {
	hash, err := hashPassword("StrongPassword1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "StrongPassword1") || !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("password stored unsafely")
	}
	if !verifyPassword(hash, "StrongPassword1") || verifyPassword(hash, "WrongPassword1") {
		t.Fatal("password verification incorrect")
	}
	for _, malformed := range []string{"", "password", "$argon2id$v=19$m=9999999999,t=2,p=1$AA$AA", "$argon2id$v=19$m=19456,t=2,p=1$!$!"} {
		if verifyPassword(malformed, "a") {
			t.Fatal("accepted malformed hash")
		}
	}
}

func TestAccessTokenRejectsTamperingWrongKeyAndExpiry(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	c := claims{Subject: "user", Session: "family", Role: "user", Version: 3, Expires: now.Add(30 * time.Minute).Unix()}
	token, err := signAccess(key, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyAccess(key.Public().(ed25519.PublicKey), token, now)
	if err != nil || got.Subject != "user" || got.Version != 3 {
		t.Fatalf("verify: %+v %v", got, err)
	}
	if _, err = verifyAccess(key.Public().(ed25519.PublicKey), token, now.Add(30*time.Minute)); err == nil {
		t.Fatal("accepted expired token")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = verifyAccess(other, token, now); err == nil {
		t.Fatal("accepted wrong key")
	}
	parts := strings.Split(token, ".")
	parts[1] = "e30"
	if _, err = verifyAccess(key.Public().(ed25519.PublicKey), strings.Join(parts, "."), now); err == nil {
		t.Fatal("accepted tampered payload")
	}
	if _, err = verifyAccess(key.Public().(ed25519.PublicKey), "eyJhbGciOiJub25lIn0.e30.", now); err == nil {
		t.Fatal("accepted alg none")
	}
}
