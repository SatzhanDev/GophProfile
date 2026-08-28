package postgres

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Драйверы регистрируют себя через init() при импорте — самих
	// функций/типов из этих пакетов мы напрямую не используем,
	// поэтому импортируем их только ради побочного эффекта регистрации.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations применяет все ещё не применённые "up"-миграции
// из папки migrationsPath к базе данных, заданной dsn.
//
// Вызывается один раз при старте cmd/server (и позже cmd/worker),
// поэтому в докере таблица будет готова ещё до первого запроса.
func RunMigrations(migrationsPath, dsn string) error {
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), dsn)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	// Close у golang-migrate возвращает сразу две ошибки (source, database) —
	// обе осознанно игнорируем, они нас не интересуют после того, как
	// миграции уже применены (или не применились — тогда мы уже вернули
	// содержательную ошибку из m.Up() выше).
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
