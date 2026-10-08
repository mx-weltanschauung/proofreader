package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Config configures an S3-compatible storage backend (e.g. SeaweedFS).
type S3Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	UsePathStyle   bool
}

// S3Storage implements Storage against an S3-compatible endpoint.
type S3Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

// NewS3Storage builds an S3-backed Storage. It does not perform network I/O.
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage: bucket is required")
	}
	publicEndpoint := cfg.PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = cfg.Endpoint
	}
	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: cfg.UsePathStyle,
		Credentials:  creds,
	})
	presignBase := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(publicEndpoint),
		UsePathStyle: cfg.UsePathStyle,
		Credentials:  creds,
	})
	return &S3Storage{
		client:    client,
		presigner: s3.NewPresignClient(presignBase),
		bucket:    cfg.Bucket,
	}, nil
}

// EnsureBucket creates the bucket if it does not already exist.
func (s *S3Storage) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err == nil {
		return nil
	}
	var nf *types.NotFound
	var apiErr smithy.APIError
	if errors.As(err, &nf) ||
		(errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchBucket")) {
		_, cerr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
		return cerr
	}
	return err
}

// SetCORS разрешает браузеру с этих origin PUT/GET/HEAD в бакет: запись
// человека заливается presigned PUT прямо со страницы, а внешний S3 без
// правил отвечает на preflight 403. Пустой список — ничего не делать, в сеть
// не ходить: местный SeaweedFS отражает любой origin сам. Вызов идемпотентен —
// правила бакета заменяются целиком.
func (s *S3Storage) SetCORS(ctx context.Context, origins []string) error {
	if len(origins) == 0 {
		return nil
	}
	_, err := s.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: &s.bucket,
		CORSConfiguration: &types.CORSConfiguration{
			CORSRules: []types.CORSRule{{
				AllowedOrigins: origins,
				AllowedMethods: []string{"PUT", "GET", "HEAD"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{"ETag"},
				MaxAgeSeconds:  aws.Int32(3600),
			}},
		},
	})
	return err
}

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	// SigV4 signing needs a seekable body to compute the payload hash. Files
	// (the common large-object case) already are; buffer anything that isn't
	// so callers may pass any io.Reader without the request failing to sign.
	body, ok := r.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
		size = int64(len(data))
	}
	in := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   body,
	}
	if size >= 0 {
		in.ContentLength = aws.Int64(size)
	}
	if contentType != "" {
		in.ContentType = &contentType
	}
	_, err := s.client.PutObject(ctx, in)
	return err
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: &s.bucket, Key: &key},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3Storage) PresignGetAttachment(ctx context.Context, key string, ttl time.Duration, disposition string) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: &s.bucket, Key: &key, ResponseContentDisposition: &disposition},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}

func (s *S3Storage) DeletePrefix(ctx context.Context, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &prefix,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: &s.bucket, Key: obj.Key,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	req, err := s.presigner.PresignPutObject(ctx,
		&s3.PutObjectInput{Bucket: &s.bucket, Key: &key, ContentType: &contentType},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3Storage) Head(ctx context.Context, key string) (int64, string, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		var nf *types.NotFound
		var apiErr smithy.APIError
		if errors.As(err, &nf) ||
			(errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey")) {
			return 0, "", fmt.Errorf("%w: %s", ErrObjectNotFound, key)
		}
		return 0, "", err
	}
	etag := strings.ToLower(strings.Trim(aws.ToString(out.ETag), `"`))
	return aws.ToInt64(out.ContentLength), etag, nil
}

// HasPrefix reports whether at least one object key starts with prefix. One
// key is enough: Fallback asks "is anything of works/N/ here", not for the
// whole listing of an 800-page volume.
func (s *S3Storage) HasPrefix(ctx context.Context, prefix string) (bool, error) {
	out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  &s.bucket,
		Prefix:  &prefix,
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return false, err
	}
	return len(out.Contents) > 0, nil
}
