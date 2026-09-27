// Package postgres содержит инфраструктурный код для работы с PostgreSQL:
// создание пула соединений и применение миграций. Здесь нет бизнес-логики —
// только "сантехника", поэтому пакет лежит в pkg, а не в internal/repository.
// Им пользуются и cmd/server, и cmd/worker.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SatzhanDev/GophProfile/pkg/telemetry"
)

// NewPool создаёт пул соединений с PostgreSQL и сразу проверяет его
// пингом — так ошибка неверного DSN или недоступной БД всплывает
// при старте приложения, а не при первом запросе от пользователя.
//
// Пулу подключается telemetry.PgxTracer — после этого КАЖДЫЙ SQL-запрос,
// сделанный через этот пул (из любого метода репозитория), автоматически
// оборачивается в span "db.query" с текстом самого запроса. Оборачивать
// каждый метод репозитория вручную не нужно — это подключается один раз здесь.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.ConnConfig.Tracer = telemetry.NewPgxTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
