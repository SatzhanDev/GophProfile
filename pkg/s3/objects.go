package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracer — общий на весь пакет, как и принято в OpenTelemetry: не нужно
// протаскивать его через конструктор Client отдельным полем.
var tracer = otel.Tracer("s3")

// Upload загружает содержимое reader в объект с ключом key.
// size — точный размер данных в байтах: он нужен minio-go, чтобы не
// буферизовать весь файл в памяти при загрузке, а стримить его сразу.
func (c *Client) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	ctx, span := tracer.Start(ctx, "s3.upload",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("s3.bucket", c.bucket),
			attribute.String("s3.key", key),
			attribute.Int64("s3.size_bytes", size),
		),
	)
	defer span.End()

	_, err := c.mc.PutObject(ctx, c.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("upload object %q: %w", key, err)
	}
	return nil
}

// Download возвращает поток с содержимым объекта. Вызывающий код обязан
// закрыть возвращённый io.ReadCloser (defer obj.Close()).
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, span := tracer.Start(ctx, "s3.download",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("s3.bucket", c.bucket),
			attribute.String("s3.key", key),
		),
	)
	defer span.End()

	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("download object %q: %w", key, err)
	}

	// GetObject сам по себе ленивый и не делает запрос сразу — ошибка
	// "объекта нет" всплыла бы только при первом Read(). Stat() форсирует
	// запрос сейчас, чтобы вызывающий код сразу получил осмысленную ошибку
	// (и чтобы сам span увидел реальный результат, а не мнимый успех).
	if _, err := obj.Stat(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		_ = obj.Close()
		return nil, fmt.Errorf("stat object %q: %w", key, err)
	}

	return obj, nil
}

// Delete удаляет объект по ключу. Если объекта уже нет — MinIO не считает
// это ошибкой, поэтому и мы не будем: операция идемпотентна.
func (c *Client) Delete(ctx context.Context, key string) error {
	ctx, span := tracer.Start(ctx, "s3.delete",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("s3.bucket", c.bucket),
			attribute.String("s3.key", key),
		),
	)
	defer span.End()

	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}
