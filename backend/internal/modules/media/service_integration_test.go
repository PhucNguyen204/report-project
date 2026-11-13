//go:build integration

package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
)

func setupMedia(t *testing.T) (*Service, *platform.DB, *S3Store, string, string) {
	t.Helper()
	db := testutil.NewDB(t)
	ctx := context.Background()
	user, sid := platform.ID(), platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Người dùng','unused','active')", user, user+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Nhà',$2)", sid, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", sid, user); err != nil {
		t.Fatal(err)
	}
	client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(testutil.S3Endpoint()), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", "")})
	bucket := "media-" + user
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		t.Fatalf("real S3 required: %v", err)
	}
	store := NewS3(client, bucket)
	t.Cleanup(func() {
		out, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &bucket})
		if out != nil {
			for _, o := range out.Contents {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: o.Key})
			}
		}
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket})
	})
	return New(db, store), db, store, user, sid
}
func picture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{200, 50, 60, 255})
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func putImage(t *testing.T, g Grant, b []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, g.URL, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range g.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %d %s", resp.StatusCode, body)
	}
}

// Removing ownership or immutable promotion would allow an unrelated or later upload to replace the bound image.
func TestRealS3UploadFinalizesPrivateOwnedImage(t *testing.T) {
	s, db, store, user, _ := setupMedia(t)
	ctx := context.Background()
	data := picture(t)
	g, err := s.Upload(ctx, user, UploadInput{Purpose: "avatar", TargetID: user, ContentType: "image/png", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	if g.ObjectID == "" || g.ExpiresAt.Sub(db.Clock.Now()) > 5*time.Minute+time.Second {
		t.Fatalf("invalid grant %+v", g)
	}
	putImage(t, g, data)
	if err = s.Complete(ctx, platform.ID(), g.ObjectID); err == nil {
		t.Fatal("foreign user completed image")
	}
	if err = s.Complete(ctx, user, g.ObjectID); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.Validate(ctx, tx, user, "avatar", user, g.ObjectID) }); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.Validate(ctx, tx, user, "recipe", platform.ID(), g.ObjectID) }); err == nil {
		t.Fatal("image reused for wrong purpose")
	}
	var key string
	if err = db.Pool.QueryRow(ctx, "SELECT object_key FROM media.objects WHERE id=$1", g.ObjectID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	putImage(t, g, bytes.Repeat([]byte("x"), len(data)))
	obj, err := store.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &store.bucket, Key: &key})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(obj.Body)
	obj.Body.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("final image changed through staging URL")
	}
	req, _ := http.NewRequest(http.MethodGet, testutil.S3Endpoint()+"/"+store.bucket+"/"+key, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("media publicly readable")
	}
	if err = s.Complete(ctx, user, g.ObjectID); err != nil {
		t.Fatalf("completion replay: %v", err)
	}
}

// Removing physical content checks would let HTML or truncated images become finalized image attachments.
func TestUploadRejectsMissingWrongAndOversizedContent(t *testing.T) {
	s, _, store, user, _ := setupMedia(t)
	ctx := context.Background()
	for _, in := range []UploadInput{{"avatar", user, "text/html", 20}, {"avatar", user, "image/png", 5*1024*1024 + 1}, {"avatar", user, "image/png", 0}, {"avatar", platform.ID(), "image/png", 20}} {
		if _, err := s.Upload(ctx, user, in); err == nil {
			t.Fatalf("invalid grant accepted %+v", in)
		}
	}
	data := []byte("<script>unsafe</script>")
	g, err := s.Upload(ctx, user, UploadInput{"avatar", user, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("missing object accepted")
	}
	putImage(t, g, data)
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("wrong bytes accepted")
	}
	good := picture(t)
	g, err = s.Upload(ctx, user, UploadInput{"avatar", user, "image/png", int64(len(good) + 1)})
	if err != nil {
		t.Fatal(err)
	}
	// A privileged staging overwrite bypasses the signed content-length check.
	u, _ := url.Parse(g.URL)
	if err = store.Put(ctx, strings.TrimPrefix(u.Path, "/"+store.bucket+"/"), "image/png", good); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

// Removing complete-time role checks would allow a former leader to finalize new space media.
func TestUploadRechecksTargetAuthority(t *testing.T) {
	s, db, _, user, sid := setupMedia(t)
	ctx := context.Background()
	data := picture(t)
	g, err := s.Upload(ctx, user, UploadInput{"space", sid, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, g, data)
	if _, err = db.Pool.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, user); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("revoked member finalized image")
	}
}
