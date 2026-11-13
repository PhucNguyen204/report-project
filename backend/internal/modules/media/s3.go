package media

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"time"
)

type S3Store struct {
	client *s3.Client
	bucket string
}

func NewS3(client *s3.Client, bucket string) *S3Store { return &S3Store{client, bucket} }

func (s *S3Store) uploadURL(ctx context.Context, key, contentType string, size int64) (string, map[string]string, error) {
	out, err := s3.NewPresignClient(s.client).PresignPutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, ContentType: &contentType, ContentLength: &size}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		return "", nil, err
	}
	headers := map[string]string{"Content-Type": contentType}
	for k, values := range out.SignedHeader {
		if len(values) > 0 {
			headers[k] = values[0]
		}
	}
	return out.URL, headers, nil
}
func (s *S3Store) readImage(ctx context.Context, key string) ([]byte, string, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, "", err
	}
	defer out.Body.Close()
	b, err := io.ReadAll(io.LimitReader(out.Body, 5*1024*1024+1))
	return b, aws.ToString(out.ContentType), err
}
func (s *S3Store) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, ContentType: &contentType, Body: bytes.NewReader(data)})
	return err
}
func (s *S3Store) Delete(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil {
			return err
		}
	}
	return nil
}
func (s *S3Store) DownloadURL(ctx context.Context, key, filename string, expiry time.Duration) (string, error) {
	if expiry <= 0 || expiry > 15*time.Minute {
		return "", fmt.Errorf("invalid download expiry")
	}
	input := &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}
	if filename != "" {
		input.ResponseContentDisposition = aws.String("attachment; filename=\"" + filename + "\"")
	}
	out, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}
