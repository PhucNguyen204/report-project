//go:build integration

package exports

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"dicho/internal/modules/media"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
)

func setupExport(t *testing.T) (*Service, *platform.DB, *media.S3Store, string, string) {
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
	client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String("http://127.0.0.1:59000"), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", "")})
	bucket := "exports-" + user
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		t.Fatalf("real S3 required: %v", err)
	}
	t.Cleanup(func() {
		out, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &bucket})
		if out != nil {
			for _, o := range out.Contents {
				_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: o.Key})
			}
		}
		_, _ = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket})
	})
	store := media.NewS3(client, bucket)
	return New(db, planning.New(db, nil, nil, nil), store), db, store, user, sid
}
func exportInput(db *platform.DB, format string) Input {
	return Input{ReportType: "spending", From: db.Today().Format("2006-01-02"), To: db.Today().Format("2006-01-02"), Format: format, GroupBy: "day"}
}
func exportJobs(t *testing.T, db *platform.DB, event string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM river_job WHERE args->>'event'=$1", event).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Committing the request before enqueue would leave a pending export without a worker after a queue failure.
func TestRequestIsAtomicWithRiverAndScopedToRequester(t *testing.T) {
	s, db, _, user, sid := setupExport(t)
	ctx := context.Background()
	original := db.EnqueueFunc
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, at time.Time) error {
		if err := original(ctx, tx, kind, key, payload, at); err != nil {
			return err
		}
		return errors.New("queue failed")
	}
	if _, err := s.Request(ctx, user, sid, exportInput(db, "csv")); err == nil {
		t.Fatal("queue failure hidden")
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM report.exports").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || exportJobs(t, db, "report.export") != 0 {
		t.Fatal("request/job survived rollback")
	}
	db.EnqueueFunc = original
	out, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "pending" || exportJobs(t, db, "report.export") != 1 {
		t.Fatalf("request %+v", out)
	}
	if _, err = s.Get(ctx, platform.ID(), sid, out.ID); err == nil {
		t.Fatal("outsider read export")
	}
	other := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Bạn cùng nhà','unused','active')", other, other+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','personal')", sid, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, other, sid, out.ID); err == nil {
		t.Fatal("member read another requester's export")
	}
	pending, err := s.Get(ctx, user, sid, out.ID)
	if err != nil || pending.URL != "" || pending.Status != "pending" {
		t.Fatalf("pending %+v %v", pending, err)
	}
}

// Publishing ready before S3 succeeds leaks unusable downloads; worker replay must enqueue one completion notice.
func TestExportUploadsRealCSVAndReplaysWithoutDuplicateNotice(t *testing.T) {
	s, db, _, user, sid := setupExport(t)
	ctx := context.Background()
	out, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := s.Get(ctx, user, sid, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != "ready" || ready.URL == "" || ready.ExpiresAt.Sub(db.Clock.Now()) > 7*24*time.Hour+time.Second {
		t.Fatalf("ready %+v", ready)
	}
	resp, err := http.Get(ready.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(data) < 20 {
		t.Fatalf("download %d %s", resp.StatusCode, data)
	}
	if exportJobs(t, db, "notification") != 1 {
		t.Fatal("completion notification duplicated or missing")
	}
	if _, err = db.Pool.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, user); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, user, sid, out.ID); err == nil {
		t.Fatal("revoked member downloaded export")
	}
}

type brokenStore struct{ Store }

func (b brokenStore) Put(context.Context, string, string, []byte) error {
	return errors.New("storage unavailable secret provider body")
}
func TestFailedExportCanRetryWithoutPublishingPartialFile(t *testing.T) {
	s, db, store, user, sid := setupExport(t)
	ctx := context.Background()
	out, err := s.Request(ctx, user, sid, exportInput(db, "pdf"))
	if err != nil {
		t.Fatal(err)
	}
	s.store = brokenStore{store}
	if err = s.Process(ctx, out.ID); err == nil {
		t.Fatal("provider failure hidden")
	}
	failed, err := s.Get(ctx, user, sid, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.URL != "" || failed.Error == "" || exportJobs(t, db, "notification") != 0 {
		t.Fatalf("partial export %+v", failed)
	}
	if strings.Contains(failed.Error, "secret") || strings.Contains(failed.Error, "provider") {
		t.Fatal("provider error leaked")
	}
	s.store = store
	if err = s.Process(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := s.Get(ctx, user, sid, out.ID)
	if err != nil || ready.Status != "ready" || ready.Error != "" {
		t.Fatalf("retry %+v %v", ready, err)
	}
	response, err := http.Get(ready.URL)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.HasPrefix(string(pdf), "%PDF-") || response.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("PDF download %d %s %v", response.StatusCode, response.Header.Get("Content-Type"), err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE report.exports SET expires_at=$2 WHERE id=$1", out.ID, db.Clock.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, user, sid, out.ID); err == nil {
		t.Fatal("expired export downloadable")
	}
}

type revokingStore struct {
	Store
	db        *platform.DB
	user, sid string
}

func (s revokingStore) Put(ctx context.Context, key, kind string, b []byte) error {
	if err := s.Store.Put(ctx, key, kind, b); err != nil {
		return err
	}
	_, err := s.db.Pool.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", s.sid, s.user)
	return err
}

// Removing final authorization would publish a file after the requester loses membership during upload.
func TestWorkerRechecksMembershipAfterRealS3Upload(t *testing.T) {
	s, db, store, user, sid := setupExport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	s.store = revokingStore{store, db, user, sid}
	if err = s.Process(ctx, out.ID); err == nil {
		t.Fatal("revocation during upload ignored")
	}
	var status string
	var key *string
	if err = db.Pool.QueryRow(ctx, "SELECT status,object_key FROM report.exports WHERE id=$1", out.ID).Scan(&status, &key); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || key != nil || exportJobs(t, db, "notification") != 0 {
		t.Fatalf("unauthorized publication %s %v", status, key)
	}
	if exportJobs(t, db, "export.delete") != 1 {
		t.Fatal("uploaded but unpublished blob has no durable cleanup")
	}
	if _, err = s.Request(ctx, user, sid, exportInput(db, "csv")); err == nil {
		t.Fatal("former member requested another export")
	}
}

type gatedStore struct {
	Store
	entered chan struct{}
	release chan struct{}
}

func (s gatedStore) Put(ctx context.Context, key, kind string, b []byte) error {
	if err := s.Store.Put(ctx, key, kind, b); err != nil {
		return err
	}
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Concurrent worker replays must select one immutable final object and enqueue only one completion notice.
func TestConcurrentWorkersPublishOneCompletion(t *testing.T) {
	s, db, store, user, sid := setupExport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	in := exportInput(db, "csv")
	in.GroupBy = ""
	out, err := s.Request(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}, 2), make(chan struct{})
	s.store = gatedStore{store, entered, release}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- s.Process(ctx, out.ID) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("worker upload did not reach barrier")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	ready, err := s.Get(ctx, user, sid, out.ID)
	if err != nil || ready.Status != "ready" || exportJobs(t, db, "notification") != 1 {
		t.Fatalf("concurrent ready %+v %v", ready, err)
	}
}
