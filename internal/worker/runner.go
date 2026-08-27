// Package worker содержит обработчики событий из RabbitMQ: генерацию
// миниатюр после загрузки и удаление файлов из S3 после мягкого удаления
// записи в БД.
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	retryAttempts = 3
	retryBaseWait = time.Second
)

// RunLoop читает сообщения из deliveries, разбирает JSON-тело в T и
// передаёт в handle. Дженерик здесь ровно для того, чтобы не копировать
// этот цикл отдельно для AvatarUploadEvent и AvatarDeleteEvent — вся
// логика ретраев и подтверждений у них одинаковая, отличается только тип
// события и сам обработчик.
//
// Цикл завершается сам, когда канал deliveries закрывается — это происходит
// при вызове Consumer.Close() из main(), поэтому отдельный ctx для остановки
// самого цикла не нужен, ctx прокидывается только в сам handle.
func RunLoop[T any](ctx context.Context, name string, deliveries <-chan amqp.Delivery, handle func(context.Context, T) error) {
	for d := range deliveries {
		var event T
		if err := json.Unmarshal(d.Body, &event); err != nil {
			slog.Error("failed to unmarshal event, dropping message", "consumer", name, "error", err)
			// Сообщение синтаксически битое — повторная доставка его не
			// починит, поэтому не возвращаем в очередь (requeue=false).
			_ = d.Nack(false, false)
			continue
		}

		handleErr := runWithRetry(ctx, name, handle, event)

		if handleErr != nil {
			slog.Error("handler failed after all retries, giving up",
				"consumer", name, "message_id", d.MessageId, "error", handleErr)
		}

		// Подтверждаем сообщение в любом случае. Свой ретрай мы уже
		// исчерпали внутри runWithRetry; если бы мы сделали Nack с
		// requeue=true, RabbitMQ тут же прислал бы то же сообщение снова
		// и мы бы зациклились на одном и том же сбое. При окончательной
		// неудаче статус аватарки останется "failed" — это видно через
		// API, а очередь при этом не блокируется для остальных сообщений.
		_ = d.Ack(false)
	}

	slog.Info("consumer loop stopped", "consumer", name)
}

// runWithRetry вызывает handle до retryAttempts раз с экспоненциальной
// паузой между попытками (1с, 2с, 4с) — как требует ТЗ.
func runWithRetry[T any](ctx context.Context, name string, handle func(context.Context, T) error, event T) error {
	var err error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		err = handle(ctx, event)
		if err == nil {
			return nil
		}

		slog.Warn("handler failed, will retry", "consumer", name, "attempt", attempt, "error", err)
		if attempt < retryAttempts {
			time.Sleep(retryBaseWait * time.Duration(1<<(attempt-1))) // 1s, 2s, 4s...
		}
	}
	return err
}
