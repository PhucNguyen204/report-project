package accounts

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dicho/internal/platform"
	"golang.org/x/crypto/argon2"
)

func validateRegistration(name, email, password string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n < 2 || n > 50 {
		return platform.Invalid("Họ tên phải có 2–50 ký tự")
	}
	if len(email) > 100 || strings.TrimSpace(email) != email {
		return platform.Invalid("Email không hợp lệ")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !strings.Contains(email, ".") {
		return platform.Invalid("Email không hợp lệ")
	}
	return validatePassword(password)
}
func validatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	upper, lower, digit := false, false, false
	for _, r := range password {
		upper = upper || unicode.IsUpper(r)
		lower = lower || unicode.IsLower(r)
		digit = digit || unicode.IsDigit(r)
	}
	if n < 8 || n > 32 || !upper || !lower || !digit {
		return platform.Invalid("Mật khẩu phải có 8–32 ký tự, chữ hoa, chữ thường và chữ số")
	}
	return nil
}
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func verifyPassword(encoded, password string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[1] != "argon2id" || p[2] != "v=19" {
		return false
	}
	var memory, iterations uint32
	var parallel uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallel); err != nil || memory < 8192 || memory > 65536 || iterations < 1 || iterations > 5 || parallel < 1 || parallel > 4 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(p[4])
	want, e2 := base64.RawStdEncoding.DecodeString(p[5])
	if e1 != nil || e2 != nil || len(salt) < 8 || len(salt) > 64 || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallel, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

type claims struct {
	Subject string `json:"sub"`
	Session string `json:"sid"`
	Role    string `json:"role"`
	Version int64  `json:"ver"`
	Expires int64  `json:"exp"`
	Issued  int64  `json:"iat"`
	ID      string `json:"jti"`
}

func signAccess(key ed25519.PrivateKey, c claims) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("invalid signing key")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT","kid":"primary"}`))
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(b)
	return body + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(body))), nil
}
func verifyAccess(key ed25519.PublicKey, token string, now time.Time) (claims, error) {
	var c claims
	p := strings.Split(token, ".")
	invalid := platform.Err(401, 3001, "Mã truy cập không hợp lệ")
	if len(p) != 3 || len(token) > 8192 || len(key) != ed25519.PublicKeySize {
		return c, invalid
	}
	head, err := base64.RawURLEncoding.DecodeString(p[0])
	if err != nil {
		return c, invalid
	}
	var h struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Key string `json:"kid"`
	}
	if json.Unmarshal(head, &h) != nil || h.Alg != "EdDSA" || h.Typ != "JWT" || h.Key != "primary" {
		return c, invalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[2])
	if err != nil || !ed25519.Verify(key, []byte(p[0]+"."+p[1]), sig) {
		return c, invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(p[1])
	if err != nil || json.Unmarshal(payload, &c) != nil || c.Subject == "" || c.Session == "" || (c.Role != "user" && c.Role != "admin") {
		return c, invalid
	}
	if c.Expires <= now.Unix() {
		return c, platform.Err(401, 3002, "Mã truy cập đã hết hạn")
	}
	return c, nil
}
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func randomOTP() (string, error) {
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		n := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
		if n < 4294000000 {
			return fmt.Sprintf("%06s", strconv.FormatUint(n%1000000, 10)), nil
		}
	}
}
