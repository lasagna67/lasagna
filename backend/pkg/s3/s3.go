package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/lasagna67/lasagna.git/internal/config"
	"golang.org/x/sync/errgroup"
)

const tagLookupConcurrency = 16

type UploadURL struct {
	URL string `json:"url"`
	Key string `json:"key"`
	// Headers — подписанные заголовки (Content-Type, X-Amz-Tagging), которые клиент обязан отправить
	// в PUT как есть: другой Content-Type — 403, без X-Amz-Tagging — 403 в AWS, 400 в MinIO.
	// Host и Content-Length тоже подписаны, но их браузер выставит сам — из URL и размера тела.
	Headers map[string]string `json:"headers"`
}

type S3Client struct {
	client   *s3.Client
	presign  *s3.PresignClient
	uploader *manager.Uploader

	buckets     []string
	pingTimeout time.Duration
}

func NewS3Client(cfg config.Config) (*S3Client, error) {
	cred := credentials.NewStaticCredentialsProvider(cfg.S3.AccessKey, cfg.S3.SecretKey, "")
	awsCfg, err := awscfg.LoadDefaultConfig(
		context.Background(),
		awscfg.WithRegion(cfg.S3.Region),
		awscfg.WithBaseEndpoint(cfg.S3.Endpoint),
		awscfg.WithCredentialsProvider(cred),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	// Ссылки открывает браузер, а Host входит в подпись — заменить адрес в готовой ссылке нельзя,
	// поэтому подписываем сразу публичным. Подпись считается локально, в сеть этот клиент не ходит.
	presignClient := client
	if cfg.S3.PublicEndpoint != "" {
		presignClient = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.UsePathStyle = true
			o.BaseEndpoint = &cfg.S3.PublicEndpoint
		})
	}

	return &S3Client{
		client:      client,
		uploader:    manager.NewUploader(client),
		presign:     s3.NewPresignClient(presignClient),
		buckets:     cfg.S3.Buckets,
		pingTimeout: cfg.S3.PingTimeout,
	}, nil
}

// Healthy проверяет бакеты из конфига через HeadBucket — хватает прав на сами бакеты.
// Без бакетов в конфиге — ListBuckets, ему нужны права на список всех бакетов.
func (s *S3Client) Healthy(ctx context.Context) error {
	if s.pingTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.pingTimeout)
		defer cancel()
	}

	if len(s.buckets) == 0 {
		if _, err := s.client.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
			return fmt.Errorf("ping s3: %w", err)
		}
		return nil
	}

	for _, bucket := range s.buckets {
		if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket}); err != nil {
			return fmt.Errorf("ping s3 bucket %q: %w", bucket, err)
		}
	}
	return nil
}

// PresignUpload — ссылка на PUT ровно size байт, в подпись которой входят метки объекта. Объекта ещё нет,
// поэтому метку ставит сам PUT клиента: PutObjectTagging до загрузки отвечает NoSuchKey.
// Метка state=temporary + lifecycle-правило бакета удаляют брошенные загрузки.
// Размер подписан: тело другой длины S3 отклонит, так что лимит на size проверяет вызывающий.
func (s *S3Client) PresignUpload(
	ctx context.Context,
	bucket string,
	key string,
	contentType string,
	size int64,
	tags map[string]string,
	expires time.Duration,
) (*UploadURL, error) {
	if size <= 0 {
		return nil, fmt.Errorf("presign upload: size must be positive, got %d", size)
	}

	input := &s3.PutObjectInput{
		Bucket:        &bucket,
		Key:           &key,
		ContentType:   &contentType,
		ContentLength: &size,
	}
	if len(tags) > 0 {
		v := url.Values{}
		for k, val := range tags {
			v.Set(k, val)
		}
		tagging := v.Encode()
		input.Tagging = &tagging
	}

	req, err := s.presign.PresignPutObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = expires
	})
	if err != nil {
		return nil, fmt.Errorf("presign upload: %w", err)
	}

	headers := make(map[string]string, len(req.SignedHeader))
	for k, v := range req.SignedHeader {
		switch http.CanonicalHeaderKey(k) {
		case "Host", "Content-Length": // выставит сам клиент; браузер и не даст задать их вручную
			continue
		}
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &UploadURL{
		Key:     key,
		URL:     req.URL,
		Headers: headers,
	}, nil
}

func (s *S3Client) PresignDownload(
	ctx context.Context,
	bucket string,
	key string,
	expires time.Duration,
) (string, error) {
	req, err := s.presign.PresignGetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket: &bucket,
			Key:    &key,
		},
		func(opts *s3.PresignOptions) {
			opts.Expires = expires
		},
	)
	if err != nil {
		return "", fmt.Errorf("presign download: %w", err)
	}

	return req.URL, nil
}

// // С гарантией позиции
// func (s *S3Client) PresignArrayDownload(ctx context.Context, keys []string) ([]string, error) {

// }

func (s *S3Client) Delete(
	ctx context.Context,
	bucket string,
	key string,
) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}

	return nil
}

func (s *S3Client) Exists(
	ctx context.Context,
	bucket string,
	key string,
) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}

	return false, fmt.Errorf("head object: %w", err)
}

// isNotFound — объекта нет: HeadObject отвечает NotFound (у HEAD нет тела), остальные — NoSuchKey.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		(apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey")
}

func (s *S3Client) Upload(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
) error {
	_, err := s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &key,
		Body:        body,
		ContentType: &contentType,
	})

	if err != nil {
		return fmt.Errorf("upload object: %w", err)
	}

	return nil
}

func (s *S3Client) Download(
	ctx context.Context,
	bucket string,
	key string,
) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("download object: %w", err)
	}

	return obj.Body, nil
}

func (s *S3Client) SetTags(
	ctx context.Context,
	bucket string,
	key string,
	tags map[string]string,
) error {
	tagSet := make([]types.Tag, 0, len(tags))

	for k, v := range tags {
		tagSet = append(tagSet, types.Tag{
			Key:   &k,
			Value: &v,
		})
	}

	_, err := s.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: &bucket,
		Key:    &key,
		Tagging: &types.Tagging{
			TagSet: tagSet,
		},
	})
	if err != nil {
		return fmt.Errorf("set object tags: %w", err)
	}

	return nil
}

// HeadObject — получает метаданные объекта без скачивания
func (s *S3Client) HeadObject(
	ctx context.Context,
	bucket string,
	key string,
) (*s3.HeadObjectOutput, error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("head object: %w", err)
	}
	return output, nil
}

// ListObjectsByTag — список объектов по тегу (для очистки временных файлов).
// В листинге S3 тегов нет, поэтому на каждый объект бакета уходит GetObjectTagging —
// параллельно, но не больше tagLookupConcurrency запросов разом.
func (s *S3Client) ListObjectsByTag(
	ctx context.Context,
	bucket string,
	tagKey string,
	tagValue string,
) ([]string, error) {
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &bucket})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}

		matched := make([]bool, len(page.Contents)) // по индексу — чтобы сохранить порядок листинга
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(tagLookupConcurrency)
		for i, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			g.Go(func() error {
				tagging, err := s.client.GetObjectTagging(gctx, &s3.GetObjectTaggingInput{
					Bucket: &bucket,
					Key:    obj.Key,
				})
				if isNotFound(err) {
					return nil // объект удалили между листингом и чтением тегов
				}
				if err != nil {
					return fmt.Errorf("get object tagging %q: %w", *obj.Key, err)
				}
				matched[i] = hasTag(tagging.TagSet, tagKey, tagValue)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}

		for i, obj := range page.Contents {
			if matched[i] {
				keys = append(keys, *obj.Key)
			}
		}
	}

	return keys, nil
}

func hasTag(tagSet []types.Tag, key, value string) bool {
	for _, tag := range tagSet {
		if tag.Key != nil && tag.Value != nil && *tag.Key == key && *tag.Value == value {
			return true
		}
	}
	return false
}

// GetObjectTagging — получает теги объекта
func (s *S3Client) GetObjectTagging(
	ctx context.Context,
	bucket string,
	key string,
) (*s3.GetObjectTaggingOutput, error) {
	output, err := s.client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("get object tagging: %w", err)
	}
	return output, nil
}
