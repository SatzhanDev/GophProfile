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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/SatzhanDev/GophProfile/internal/api"
	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/internal/handlers"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/services"
	"github.com/SatzhanDev/GophProfile/pkg/broker"
	"github.com/SatzhanDev/GophProfile/pkg/metrics"
	"github.com/SatzhanDev/GophProfile/pkg/postgres"
	"github.com/SatzhanDev/GophProfile/pkg/s3"
	"github.com/SatzhanDev/GophProfile/pkg/telemetry"
	"github.com/SatzhanDev/GophProfile/web"
)

// defaultServiceName — имя, под которым server будет виден в Jaeger,
// если OTEL_SERVICE_NAME не задан явно. У worker (cmd/worker/main.go) —
// своё имя по умолчанию, чтобы в Jaeger трейсы двух процессов не путались.
const defaultServiceName = "gophprofile-server"

func main() {
	// .env — необязательный файл для локальной разработки. В докере/проде
	// переменные окружения обычно задаются напрямую, файла там нет —
	// поэтому ошибку "файл не найден" не считаем фатальной, а просто логируем
	// чуть ниже, когда логгер уже будет настроен.
	envErr := godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Логгер оборачиваем в telemetry.TraceHandler ещё до InitTracer — сам
	// TraceHandler ничего не знает про конкретный трейсер, он просто читает
	// span из контекста на каждый вызов slog.*Context(ctx, ...); если в ctx
	// спана ещё нет (или он невалиден), просто не добавляет trace_id/span_id.
	logger := slog.New(telemetry.NewTraceHandler(slog.NewJSONHandler(os.Stdout, nil)))
	slog.SetDefault(logger)

	if envErr != nil {
		slog.Debug("no .env file found, using process environment", "error", envErr)
	}

	// Отдельный контекст для операций старта (подключение к БД, миграции) —
	// он не связан с ctx graceful shutdown, который появится чуть ниже.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	serviceName := cfg.Telemetry.ServiceName
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	shutdownTracer, err := telemetry.InitTracer(startupCtx, serviceName, cfg.Telemetry.OTLPEndpoint)
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		// На shutdown — отдельный, уже не отменённый контекст: если использовать
		// startupCtx, к моменту остановки приложения он уже истечёт сам по себе.
		if err := shutdownTracer(context.Background()); err != nil {
			slog.Error("failed to shutdown tracer", "error", err)
		}
	}()

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

	webHandler, err := handlers.NewWebHandler(avatarService, web.TemplatesFS, web.StaticFS)
	if err != nil {
		slog.Error("failed to init web handler", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.Deps{
		Config:        cfg,
		HealthChecks:  healthChecks,
		AvatarHandler: avatarHandler,
		WebHandler:    webHandler,
	})

	// otelhttp оборачивает весь роутер целиком: на каждый входящий запрос
	// создаёт span (с методом, путём, кодом ответа в атрибутах) и,
	// что важно, сначала пытается извлечь заголовок "traceparent" из
	// запроса — если сервис вызвали не напрямую, а из системы, которая
	// уже ведёт свой трейсинг, наш span станет продолжением ЕЁ трейса,
	// а не начнёт новый с нуля.
	instrumentedRouter := otelhttp.NewHandler(router, "gophprofile.server")

	srv := &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           instrumentedRouter,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Контекст, который отменяется при получении SIGINT/SIGTERM —
	// это позволяет сделать graceful shutdown вместо мгновенного убийства процесса.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Периодически публикуем состояние пула соединений с Postgres —
	// см. metrics.CollectDBPoolStats. Останавливается сам вместе с ctx.
	go metrics.CollectDBPoolStats(ctx, dbPool, 15*time.Second)

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
