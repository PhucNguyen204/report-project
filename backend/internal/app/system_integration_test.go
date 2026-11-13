//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"dicho/internal/modules/media"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

type capturedEmail struct{ to, body string }
type inbox struct{ mail chan capturedEmail }

func (i *inbox) Send(_ context.Context, to, subject, body string) error {
	i.mail <- capturedEmail{to, body}
	return nil
}
func systemFixture(t *testing.T) (*System, *inbox) {
	t.Helper()
	db := testutil.NewDB(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(testutil.S3Endpoint()); o.UsePathStyle = true })
	mail := &inbox{mail: make(chan capturedEmail, 10)}
	sys, err := NewSystem(context.Background(), db, Options{SigningKey: key, QueueKey: bytes.Repeat([]byte{7}, 32), MediaStore: media.NewS3(client, "media"), ExportStore: media.NewS3(client, "exports"), Email: mail, HTTP: Config{MinimumAppVersion: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = sys.Worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sys.Worker.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return sys, mail
}
func systemCall(t *testing.T, sys *System, method, path string, body any, token, key string) (int, map[string]any) {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("X-App-Version", "1.0.0")
	if len(data) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	sys.Handler.ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON%d %s", w.Code, w.Body)
	}
	return w.Code, out
}
func systemUser(t *testing.T, sys *System, mail *inbox, email string) (string, string, string) {
	t.Helper()
	status, out := systemCall(t, sys, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Test User", "email": email, "password": "Password123"}, "", "")
	if status != 201 {
		t.Fatalf("register%d %v", status, out)
	}
	var code string
	select {
	case m := <-mail.mail:
		if m.to != email {
			t.Fatal("email sent to wrong recipient")
		}
		code = regexp.MustCompile(`\b[0-9]{6}\b`).FindString(m.body)
	case <-time.After(8 * time.Second):
		t.Fatal("transactional email worker did not deliver")
	}
	if code == "" {
		t.Fatal("OTP absent from delivered email")
	}
	status, out = systemCall(t, sys, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": code, "purpose": "register"}, "", "")
	if status != 200 {
		t.Fatalf("verify%d %v", status, out)
	}
	data := out["data"].(map[string]any)
	return data["accessToken"].(string), data["spaceId"].(string), data["userId"].(string)
}
func TestSystemGatewayPersistsAndReplaysAuthenticatedCommands(t *testing.T) {
	sys, mail := systemFixture(t)
	token, sid, _ := systemUser(t, sys, mail, "owner@example.test")
	other, _, _ := systemUser(t, sys, mail, "other@example.test")
	body := map[string]any{"name": "Weekly shopping", "plannedDate": sys.DB.Today().Format("2006-01-02")}
	path := "/it4788/v1/spaces/" + sid + "/shopping-lists"
	key := platform.ID()
	status, out := systemCall(t, sys, "POST", path, body, token, key)
	if status != 201 {
		t.Fatalf("create%d %v", status, out)
	}
	id := out["data"].(map[string]any)["id"].(string)
	status, replayed := systemCall(t, sys, "POST", path, body, token, key)
	if status != 201 || replayed["data"].(map[string]any)["id"] != id {
		t.Fatalf("replay%d %v", status, replayed)
	}
	status, _ = systemCall(t, sys, "GET", path+"/"+id, nil, other, "")
	if status != 404 {
		t.Fatalf("cross-space%d", status)
	}
	var args string
	if err := sys.DB.Pool.QueryRow(context.Background(), "SELECT args::text FROM river_job WHERE args->>'event'='email' LIMIT 1").Scan(&args); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(args, "owner@example.test") || strings.Contains(args, "code\":") {
		t.Fatal("outbound secrets persisted unencrypted")
	}
	var count int
	_ = sys.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1", sid).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate write%d", count)
	}
}
