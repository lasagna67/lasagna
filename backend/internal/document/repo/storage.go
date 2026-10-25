package documentrepo

import (
	"bytes"
	"context"
	"io"

	"github.com/lasagna67/lasagna.git/pkg/s3"
)

// Storage — файлы заявок в бакете S3. Ключ объекта наружу не отдаётся.
type Storage struct {
	client *s3.S3Client
	bucket string
}

func NewStorage(client *s3.S3Client, bucket string) *Storage {
	return &Storage{client: client, bucket: bucket}
}

func (s *Storage) Put(ctx context.Context, key, contentType string, body []byte) error {
	return s.client.Upload(ctx, s.bucket, key, bytes.NewReader(body), contentType)
}

func (s *Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.Download(ctx, s.bucket, key)
}
