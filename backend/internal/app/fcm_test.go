package app

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type fcmTransport func(*http.Request) (*http.Response, error)

func (f fcmTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func serviceAccountFixture(t *testing.T) (map[string]string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"type": "service_account", "client_email": "fixture@unit-test.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": "https://oauth2.googleapis.com/token", "universe_domain": "googleapis.com"}, key
}

func TestFCMCredentialsRejectUntrustedConfigurationBeforeNetwork(t *testing.T) {
	fixture, _ := serviceAccountFixture(t)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: fcmTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("credential parsing performed network access")
		return nil, nil
	})})
	for _, change := range []struct{ field, value string }{
		{"type", "authorized_user"}, {"type", "external_account"}, {"type", "PRIVATE-unknown"},
		{"token_uri", "http://127.0.0.1:12345/PRIVATE"}, {"token_uri", "https://PRIVATE.example/token"},
		{"token_uri", "https://oauth2.googleapis.com/token?PRIVATE"}, {"universe_domain", "PRIVATE.example"},
		{"client_email", "PRIVATE@example.test"}, {"client_email", "PRIVATE <fixture@unit-test.iam.gserviceaccount.com>"},
		{"private_key", "PRIVATE-invalid-key"}, {"private_key", ""},
	} {
		t.Run(change.field+"/"+change.value, func(t *testing.T) {
			input := make(map[string]string)
			for k, v := range fixture {
				input[k] = v
			}
			input[change.field] = change.value
			raw, _ := json.Marshal(input)
			path := filepath.Join(t.TempDir(), "PRIVATE-credentials.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadFCMCredentials(ctx, path); err == nil || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "BEGIN") {
				t.Fatal("untrusted credentials accepted or echoed")
			}
		})
	}
}

func TestFCMCredentialsSignForGoogleWithOnlyMessagingScope(t *testing.T) {
	fixture, key := serviceAccountFixture(t)
	for _, uri := range []string{"https://oauth2.googleapis.com/token", ""} {
		fixture["token_uri"] = uri
		raw, _ := json.Marshal(fixture)
		path := filepath.Join(t.TempDir(), "credentials.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		calls := 0
		client := &http.Client{Transport: fcmTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "POST" || r.URL.String() != "https://oauth2.googleapis.com/token" || r.ParseForm() != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Fatal("wrong token request destination or grant")
			}
			parts := strings.Split(r.Form.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Fatal("missing signed JWT assertion")
			}
			claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				t.Fatal(err)
			}
			var claims map[string]any
			if err := json.Unmarshal(claimsJSON, &claims); err != nil {
				t.Fatal(err)
			}
			if claims["iss"] != "fixture@unit-test.iam.gserviceaccount.com" || claims["aud"] != "https://oauth2.googleapis.com/token" || claims["scope"] != "https://www.googleapis.com/auth/firebase.messaging" {
				t.Fatal("wrong issuer, audience or scope")
			}
			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
				t.Fatal("assertion was not signed by the configured key")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-access-token","token_type":"Bearer","expires_in":3600}`))}, nil
		})}
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
		creds, err := ReadFCMCredentials(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatal("loader fetched token eagerly")
		}
		token, err := creds.TokenSource.Token()
		if err != nil || token.AccessToken != "synthetic-access-token" || calls != 1 {
			t.Fatal("validated credentials could not obtain scoped token through fixture transport")
		}
	}
}

func TestFCMCredentialsRequirePrivateBoundedRegularFile(t *testing.T) {
	fixture, _ := serviceAccountFixture(t)
	valid, _ := json.Marshal(fixture)
	for _, tc := range []struct {
		name string
		raw  []byte
		mode os.FileMode
	}{
		{"public", valid, 0644}, {"unreadable", valid, 0000},
		{"large", append(valid, []byte(strings.Repeat(" ", 65537))...), 0600},
		{"bad-json", []byte("PRIVATE-not-json"), 0600}, {"null", []byte("null"), 0600},
	} {
		path := filepath.Join(t.TempDir(), "PRIVATE-"+tc.name)
		if err := os.WriteFile(path, tc.raw, tc.mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFCMCredentials(context.Background(), path); err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("unsafe file %s accepted or leaked", tc.name)
		}
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "PRIVATE-missing"), t.TempDir()} {
		if _, err := ReadFCMCredentials(context.Background(), path); err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("missing/nonregular file accepted or leaked")
		}
	}
}

func TestFCMCredentialsRejectWeakNonRSAAndMalformedPrivateKeys(t *testing.T) {
	fixture, _ := serviceAccountFixture(t)
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []any{weak, ec} {
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		fixture["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		raw, _ := json.Marshal(fixture)
		path := filepath.Join(t.TempDir(), "credentials.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFCMCredentials(context.Background(), path); err == nil {
			t.Fatal("weak or non-RSA key accepted")
		}
	}
	fixture["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("PRIVATE-invalid-der")}))
	raw, _ := json.Marshal(fixture)
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFCMCredentials(context.Background(), path); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("invalid DER accepted or echoed")
	}
}

func TestFCMCredentialsRejectNamedPipeWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadFCMCredentials(context.Background(), path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe accepted")
		}
	case <-time.After(time.Second):
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = syscall.Close(fd)
		}
		<-done
		t.Fatal("nonregular credential path blocked startup")
	}
}
