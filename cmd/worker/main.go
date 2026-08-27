// Команда worker асинхронно обрабатывает события об аватарках: генерирует
// миниатюры после загрузки и удаляет файлы из S3 после мягкого удаления.
// Запускается отдельным процессом от cmd/server — так тяжёлую по CPU
// работу (ресайз картинок) можно масштабировать независимо от HTTP-сервера.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/worker"
	"github.com/SatzhanDev/GophProfile/pkg/broker"
	"github.com/SatzhanDev/GophProfile/pkg/postgres"
	"github.com/SatzhanDev/GophProfile/pkg/s3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env file found, using process environment", "error", err)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	// Миграции здесь не гоняем — их уже применил cmd/server при своём
	// старте. Два процесса, независимо бегущие RunMigrations при каждом
	// перезапуске, ничего не сломают (golang-migrate это позволяет), но
	// это лишняя работа на ровном месте, раз сервер и так этим занимается.
	dbPool, err := postgres.NewPool(startupCtx, cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	s3Client, err := s3.NewClient(
		startupCtx,
		cfg.S3.Endpoint, cfg.S3.AccessKey, cfg.S3.SecretKey, cfg.S3.Bucket, cfg.S3.UseSSL,
	)
	if err != nil {
		slog.Error("failed to connect to s3", "error", err)
		os.Exit(1)
	}

	consumer, err := broker.NewConsumer(cfg.Broker.URL)
	if err != nil {
		slog.Error("failed to connect to rabbitmq", "error", err)
		os.Exit(1)
	}

	uploadDeliveries, err := consumer.DeclareQueue("avatars.thumbnails", domain.RoutingKeyAvatarUploaded)
	if err != nil {
		slog.Error("failed to declare upload queue", "error", err)
		os.Exit(1)
	}

	deleteDeliveries, err := consumer.DeclareQueue("avatars.cleanup", domain.RoutingKeyAvatarDeleted)
	if err != nil {
		slog.Error("failed to declare delete queue", "error", err)
		os.Exit(1)
	}

	avatarRepo := repository.NewPostgresAvatarRepository(dbPool)
	uploadHandler := worker.NewUploadHandler(avatarRepo, s3Client)
	deleteHandler := worker.NewDeleteHandler(s3Client)

	// ctx отменяется по SIGINT/SIGTERM — передаётся в сами обработчики
	// (для запросов к БД/S3 внутри Handle), а не управляет самим циклом
	// чтения: цикл останавливается, когда закрывается канал доставок
	// (см. RunLoop и Consumer.Close ниже).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		worker.RunLoop(ctx, "upload", uploadDeliveries, uploadHandler.Handle)
	}()
	go func() {
		defer wg.Done()
		worker.RunLoop(ctx, "delete", deleteDeliveries, deleteHandler.Handle)
	}()

	slog.Info("worker started, waiting for events")
	<-ctx.Done()
	slog.Info("shutdown signal received, stopping worker")

	// Закрываем consumer — это закрывает каналы доставок, оба RunLoop
	// доканчивают текущее сообщение и естественным образом выходят из цикла.
	if err := consumer.Close(); err != nil {
		slog.Error("failed to close rabbitmq consumer", "error", err)
	}

	wg.Wait()
	slog.Info("worker stopped gracefully")
}
