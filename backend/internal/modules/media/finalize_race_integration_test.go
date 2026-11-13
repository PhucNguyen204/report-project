//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/http"
	"strings"
	"testing"
)

type revokeAfterFinalPut struct {
	db        *platform.DB
	user, sid string
}

func (c revokeAfterFinalPut) Do(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultClient.Do(r)
	if err == nil && response.StatusCode == 200 && r.Method == "PUT" && strings.Contains(r.URL.Path, "/objects/") {
		_, err = c.db.Pool.Exec(r.Context(), "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", c.sid, c.user)
	}
	return response, err
}

// Revocation after S3 PUT must leave the image unattached and queue deletion of the unpublished blob.
func TestFinalizeRevocationQueuesUnusedBlobDeletion(t *testing.T) {
	s, db, store, user, sid := setupMedia(t)
	ctx := context.Background()
	data := picture(t)
	g, err := s.Upload(ctx, user, UploadInput{"space", sid, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, g, data)
	store.client = s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String("http://127.0.0.1:59000"), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", ""), HTTPClient: revokeAfterFinalPut{db, user, sid}})
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("revocation after physical upload ignored")
	}
	var key *string
	if err = db.Pool.QueryRow(ctx, "SELECT object_key FROM media.objects WHERE id=$1", g.ObjectID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != nil {
		t.Fatal("unauthorized blob published")
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>'event'='media.delete' AND scheduled_at<=now()").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("unused blob has no durable cleanup")
	}
	if err = s.Handle(ctx, mediaEvent(t, db, "media.delete")); err != nil {
		t.Fatal(err)
	}
}
