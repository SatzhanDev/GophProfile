package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	// Ничего не задаём через env — Load должен вернуть ровно те значения
	// по умолчанию, что задокументированы в README.
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
	t.Setenv("SERVER_PORT", "not-a-number")

	_, err := Load()
	require.Error(t, err)
}

func TestLoad_InvalidBoolValue(t *testing.T) {
	t.Setenv("S3_USE_SSL", "not-a-bool")

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
