package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// Upload загружает содержимое reader в объект с ключом key.
// size — точный размер данных в байтах: он нужен minio-go, чтобы не
// буферизовать весь файл в памяти при загрузке, а стримить его сразу.
func (c *Client) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("upload object %q: %w", key, err)
	}
	return nil
}

// Download возвращает поток с содержимым объекта. Вызывающий код обязан
// закрыть возвращённый io.ReadCloser (defer obj.Close()).
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("download object %q: %w", key, err)
	}

	// GetObject сам по себе ленивый и не делает запрос сразу — ошибка
	// "объекта нет" всплыла бы только при первом Read(). Stat() форсирует
	// запрос сейчас, чтобы вызывающий код сразу получил осмысленную ошибку.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("stat object %q: %w", key, err)
	}

	return obj, nil
}

// Delete удаляет объект по ключу. Если объекта уже нет — MinIO не считает
// это ошибкой, поэтому и мы не будем: операция идемпотентна.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}
