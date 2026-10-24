package s3

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lasagna67/lasagna.git/internal/config"
)

// Метка должна входить в подпись: без X-Amz-Tagging в PUT объект ляжет без state=temporary
// и lifecycle его не удалит, а с подписанным заголовком S3 сам проверит, что клиент его прислал.
func TestPresignUploadSignsTags(t *testing.T) {
	var cfg config.Config
	cfg.S3.Endpoint = "http://localhost:9000"
	cfg.S3.Region = "us-east-1"
	cfg.S3.AccessKey, cfg.S3.SecretKey = "key", "secret"
	c, err := NewS3Client(cfg)
	if err != nil {
		t.Fatal(err)
	}

	up, err := c.PresignUpload(context.Background(), "post", "k", "image/jpeg", 1024,
		map[string]string{"state": "temporary", "type": "post_media"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got := up.Headers["X-Amz-Tagging"]; got != "state=temporary&type=post_media" {
		t.Errorf("X-Amz-Tagging = %q", got)
	}
	if got := up.Headers["Content-Type"]; got != "image/jpeg" {
		t.Errorf("Content-Type = %q: it is signed, the client must send it as is", got)
	}
	for _, h := range []string{"Host", "Content-Length"} {
		if _, ok := up.Headers[h]; ok {
			t.Errorf("%s must not be returned: the client sets it itself", h)
		}
	}

	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	signed := u.Query().Get("X-Amz-SignedHeaders")
	for _, h := range []string{"x-amz-tagging", "content-type", "content-length"} {
		if !strings.Contains(signed, h) {
			t.Errorf("%s is not signed: %q", h, signed)
		}
	}
}

// Ссылку открывает браузер: она должна указывать на публичный адрес, а не на внутренний из сети compose.
func TestPresignUsesPublicEndpoint(t *testing.T) {
	var cfg config.Config
	cfg.S3.Endpoint = "http://minio:9000"
	cfg.S3.PublicEndpoint = "http://localhost:9000"
	cfg.S3.Region = "us-east-1"
	cfg.S3.AccessKey, cfg.S3.SecretKey = "key", "secret"
	c, err := NewS3Client(cfg)
	if err != nil {
		t.Fatal(err)
	}

	up, err := c.PresignUpload(context.Background(), "post", "k", "image/jpeg", 1024, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	down, err := c.PresignDownload(context.Background(), "post", "k", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	for _, link := range []string{up.URL, down} {
		if u, err := url.Parse(link); err != nil || u.Host != "localhost:9000" {
			t.Errorf("presigned URL host = %q, want localhost:9000 (%s)", u.Host, link)
		}
	}
	if _, err := c.PresignUpload(context.Background(), "post", "k", "image/jpeg", 0, nil, time.Minute); err == nil {
		t.Error("zero size must be rejected: without Content-Length the upload size is unlimited")
	}
}
