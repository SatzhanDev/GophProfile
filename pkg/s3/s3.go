// Package s3 — инфраструктурная обёртка над S3-совместимым хранилищем
// (в разработке — MinIO, в проде им может быть AWS S3 или Yandex Object
// Storage без единой строчки изменений в остальном коде, так как все они
// говорят на одном и том же S3-протоколе).
package s3

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client оборачивает *minio.Client и знает, с каким бакетом работает.
type Client struct {
	mc     *minio.Client
	bucket string
}

// NewClient подключается к S3-совместимому хранилищу и гарантирует,
// что нужный бакет существует — создаёт его, если ещё нет. Так сервис
// можно поднять с нуля (например, в CI) без ручной настройки MinIO.
func NewClient(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*Client, error) {
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	c := &Client{mc: mc, bucket: bucket}

	if err := c.ensureBucket(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %q exists: %w", c.bucket, err)
	}
	if exists {
		return nil
	}

	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create bucket %q: %w", c.bucket, err)
	}

	return nil
}

// Ping реализует handlers.Pinger — используется health-чеком, чтобы
// показать, что S3-хранилище доступно.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.mc.BucketExists(ctx, c.bucket); err != nil {
		return fmt.Errorf("ping s3: %w", err)
	}
	return nil
}
