package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// setRequiredEnv выставляет чувствительные переменные, у которых больше
// нет дефолтов (см. getEnvRequired) — без них Load вернёт ошибку. Тестам,
// которые проверяют что-то другое, не нужно каждый раз думать про это,
// поэтому вынесено в отдельный хелпер.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PASSWORD", "test-password")
	t.Setenv("S3_ACCESS_KEY", "test-access-key")
	t.Setenv("S3_SECRET_KEY", "test-secret-key")
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
}

func TestLoad_Defaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	require.Equal(t, "development", cfg.Env)
	require.Equal(t, "0.0.0.0", cfg.Server.Host)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, 10*time.Second, cfg.Server.ShutdownTimeout)
	require.Equal(t, "gophprofile", cfg.Database.User)
	require.Equal(t, "avatars", cfg.S3.Bucket)
	require.False(t, cfg.S3.UseSSL)
	require.Equal(t, "migrations", cfg.MigrationsPath)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	setRequiredEnv(t)

	// t.Setenv сам восстанавливает исходное значение переменной после
	// завершения теста — не нужно вручную подчищать за собой.
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("SERVER_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("S3_USE_SSL", "true")

	cfg, err := Load()
	require.NoError(t, err)

	require.Equal(t, "production", cfg.Env)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, 30*time.Second, cfg.Server.ShutdownTimeout)
	require.True(t, cfg.S3.UseSSL)
}

func TestLoad_InvalidIntValue(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SERVER_PORT", "not-a-number")

	_, err := Load()
	require.Error(t, err)
}

func TestLoad_InvalidBoolValue(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("S3_USE_SSL", "not-a-bool")

	_, err := Load()
	require.Error(t, err)
}

func TestLoad_MissingRequiredEnvVar(t *testing.T) {
	// Ни одна из чувствительных переменных не задана — Load должен
	// упасть сразу и явно (fail fast), а не тихо подставить публично
	// известный дефолт вроде "minioadmin" или "guest:guest".
	_, err := Load()
	require.Error(t, err)
}

func TestDatabaseConfig_DSN(t *testing.T) {
	db := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "gophprofile",
		Password: "secret",
		Name:     "gophprofile",
		SSLMode:  "disable",
	}

	require.Equal(t, "postgres://gophprofile:secret@localhost:5432/gophprofile?sslmode=disable", db.DSN())
}
