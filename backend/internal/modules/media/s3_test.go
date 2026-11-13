package media

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicS3ClientSignsPublicURLsWhileWritesUseInternalEndpoint(t *testing.T) {
	var writes atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/media/image.png" {
			t.Errorf("unexpected internal request %s %s", r.Method, r.URL.Path)
		}
		writes.Add(1)
		w.WriteHeader(200)
	}))
	defer internal.Close()
	client := func(endpoint string) *s3.Client {
		return s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("test", "local-test", "")})
	}
	base := NewS3(client(internal.URL), "media")
	configurable, ok := any(base).(interface{ WithPublicClient(*s3.Client) *S3Store })
	if !ok {
		t.Fatal("separate public presigning client is missing")
	}
	store := configurable.WithPublicClient(client("https://media.example.test"))
	ctx := context.Background()
	upload, _, err := store.uploadURL(ctx, "image.png", "image/png", 4)
	if err != nil {
		t.Fatal(err)
	}
	download, err := store.DownloadURL(ctx, "image.png", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{upload, download} {
		u, err := url.Parse(raw)
		if err != nil || u.Host != "media.example.test" || u.Query().Get("X-Amz-Signature") == "" {
			t.Fatalf("unusable public signature %s %v", raw, err)
		}
	}
	if writes.Load() != 0 {
		t.Fatal("signing made network requests")
	}
	if err := store.Put(ctx, "image.png", "image/png", []byte{1, 2, 3, 4}); err != nil || writes.Load() != 1 {
		t.Fatalf("internal write count=%d err=%v", writes.Load(), err)
	}
	original, _, err := base.uploadURL(ctx, "image.png", "image/png", 4)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(original)
	if err != nil || "http://"+u.Host != internal.URL {
		t.Fatal("configuring a public client mutated the base store")
	}
}
