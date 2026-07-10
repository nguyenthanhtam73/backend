package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dadiary/backend/internal/config"
)

// r2Storage persists objects in a Cloudflare R2 bucket via the S3-compatible API.
//
// R2 uses path-style addressing against the account endpoint
// (https://<account>.r2.cloudflarestorage.com) and ignores region, so we pin
// region to "auto" and enable path-style requests.
type r2Storage struct {
	client *s3.Client
	bucket string
}

func newR2(cfg config.R2Config) (*r2Storage, error) {
	accountID := strings.TrimSpace(cfg.AccountID)
	accessKey := strings.TrimSpace(cfg.AccessKeyID)
	secretKey := strings.TrimSpace(cfg.SecretAccessKey)
	bucket := strings.TrimSpace(cfg.Bucket)
	if accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("storage(r2): missing DADIARY_R2_ACCESS_KEY_ID / DADIARY_R2_SECRET_ACCESS_KEY / DADIARY_R2_BUCKET")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		if accountID == "" {
			return nil, fmt.Errorf("storage(r2): set DADIARY_R2_ACCOUNT_ID or DADIARY_R2_ENDPOINT")
		}
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	}

	awsCfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return &r2Storage{client: client, bucket: bucket}, nil
}

func (r *r2Storage) Save(ctx context.Context, key string, data []byte, contentType string) error {
	k := CleanKey(key)
	in := &s3.PutObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(k),
		Body:   bytes.NewReader(data),
	}
	if ct := strings.TrimSpace(contentType); ct != "" {
		in.ContentType = aws.String(ct)
	}
	if _, err := r.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("storage(r2): put %s: %w", k, err)
	}
	return nil
}

func (r *r2Storage) Read(ctx context.Context, key string) ([]byte, error) {
	k := CleanKey(key)
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, fmt.Errorf("storage(r2): get %s: %w", k, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (r *r2Storage) DeletePrefix(ctx context.Context, prefix string) error {
	p := CleanKey(prefix)
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(p),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("storage(r2): list %s: %w", p, err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		ids := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			ids = append(ids, types.ObjectIdentifier{Key: obj.Key})
		}
		if _, err := r.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(r.bucket),
			Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		}); err != nil {
			return fmt.Errorf("storage(r2): delete batch under %s: %w", p, err)
		}
	}
	return nil
}

func (r *r2Storage) Driver() string   { return "r2" }
func (r *r2Storage) LocalDir() string { return "" }
