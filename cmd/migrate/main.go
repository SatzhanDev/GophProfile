// Команда migrate — отдельный короткоживущий бинарник, который только
// накатывает миграции БД и завершается. Существует ради Helm pre-upgrade
// hook'а (см. k8s/templates/job-migrate.yaml).
//
// cmd/server и cmd/worker тоже накатывают миграции сами при каждом
// старте (RunMigrations идемпотентен — golang-migrate помнит применённые
// номера и использует advisory lock в Postgres, так что даже если
// несколько реплик стартуют одновременно, ничего не сломается) — так что
// формально этот бинарник не обязателен для корректности. Но для прода
// явный отдельный шаг миграции — более контролируемая практика: схема
// БД обновляется один раз за релиз, до того как новые Pod'ы вообще
// начнут стартовать, а не как побочный эффект старта случайной реплики.
package main

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"

	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/pkg/postgres"
)

func main() {
	envErr := godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if envErr != nil {
		slog.Debug("no .env file found, using process environment", "error", envErr)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := postgres.RunMigrations(cfg.MigrationsPath, cfg.Database.DSN()); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	slog.Info("migrations applied successfully")
}
