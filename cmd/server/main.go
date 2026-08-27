// Команда server запускает HTTP-сервер GophProfile.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/SatzhanDev/GophProfile/internal/api"
	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/internal/handlers"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/services"
	"github.com/SatzhanDev/GophProfile/pkg/broker"
	"github.com/SatzhanDev/GophProfile/pkg/postgres"
	"github.com/SatzhanDev/GophProfile/pkg/s3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// .env — необязательный файл для локальной разработки. В докере/проде
	// переменные окружения обычно задаются напрямую, файла там нет —
	// поэтому ошибку "файл не найден" не считаем фатальной, а просто логируем.
	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env file found, using process environment", "error", err)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Отдельный контекст для операций старта (подключение к БД, миграции) —
	// он не связан с ctx graceful shutdown, который появится чуть ниже.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	dbPool, err := postgres.NewPool(startupCtx, cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	if err := postgres.RunMigrations(cfg.MigrationsPath, cfg.Database.DSN()); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	s3Client, err := s3.NewClient(
		startupCtx,
		cfg.S3.Endpoint, cfg.S3.AccessKey, cfg.S3.SecretKey, cfg.S3.Bucket, cfg.S3.UseSSL,
	)
	if err != nil {
		slog.Error("failed to connect to s3", "error", err)
		os.Exit(1)
	}
	slog.Info("s3 bucket ready", "bucket", cfg.S3.Bucket)

	publisher, err := broker.NewPublisher(cfg.Broker.URL)
	if err != nil {
		slog.Error("failed to connect to rabbitmq", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := publisher.Close(); err != nil {
			slog.Error("failed to close rabbitmq publisher", "error", err)
		}
	}()
	slog.Info("rabbitmq connected")

	healthChecks := map[string]handlers.Pinger{
		"postgres": dbPool,
		"s3":       s3Client,
		"rabbitmq": publisher,
	}

	// Слои приложения собираются снизу вверх: репозиторий (доступ к БД) →
	// сервис (бизнес-правила поверх репозитория, S3 и брокера) → хендлер (HTTP).
	avatarRepo := repository.NewPostgresAvatarRepository(dbPool)
	avatarService := services.NewAvatarService(avatarRepo, s3Client, publisher)
	avatarHandler := handlers.NewAvatarHandler(avatarService)

	router := api.NewRouter(api.Deps{
		Config:        cfg,
		HealthChecks:  healthChecks,
		AvatarHandler: avatarHandler,
	})

	srv := &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Контекст, который отменяется при получении SIGINT/SIGTERM —
	// это позволяет сделать graceful shutdown вместо мгновенного убийства процесса.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("starting server", "address", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
