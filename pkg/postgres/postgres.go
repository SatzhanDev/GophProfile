// Package postgres содержит инфраструктурный код для работы с PostgreSQL:
// создание пула соединений и применение миграций. Здесь нет бизнес-логики —
// только "сантехника", поэтому пакет лежит в pkg, а не в internal/repository.
// Им пользуются и cmd/server, и (в будущем) cmd/worker.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool создаёт пул соединений с PostgreSQL и сразу проверяет его
// пингом — так ошибка неверного DSN или недоступной БД всплывает
// при старте приложения, а не при первом запросе от пользователя.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
