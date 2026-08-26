// Package config отвечает за загрузку настроек приложения из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ServerConfig содержит настройки HTTP-сервера.
type ServerConfig struct {
	Host            string
	Port            int
	ShutdownTimeout time.Duration
}

// Address возвращает адрес в формате host:port для http.Server.
func (s ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig содержит настройки подключения к PostgreSQL.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// DSN собирает connection string в формате, который понимают
// и pgx (пул соединений), и golang-migrate (раннер миграций).
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

// Config — корневая структура конфигурации всего приложения.
// В следующих инкрементах сюда добавятся секции S3, Broker.
type Config struct {
	Env            string
	Server         ServerConfig
	Database       DatabaseConfig
	MigrationsPath string
}

// Load читает конфигурацию из переменных окружения.
// Если переменная не задана — используется значение по умолчанию,
// поэтому сервис можно запустить локально вообще без .env файла.
func Load() (*Config, error) {
	port, err := getEnvInt("SERVER_PORT", 8080)
	if err != nil {
		return nil, err
	}

	shutdownTimeout, err := getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, err
	}

	dbPort, err := getEnvInt("DB_PORT", 5434)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Env: getEnv("APP_ENV", "development"),
		Server: ServerConfig{
			Host:            getEnv("SERVER_HOST", "0.0.0.0"),
			Port:            port,
			ShutdownTimeout: shutdownTimeout,
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     getEnv("DB_USER", "gophprofile"),
			Password: getEnv("DB_PASSWORD", "gophprofile"),
			Name:     getEnv("DB_NAME", "gophprofile"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		MigrationsPath: getEnv("MIGRATIONS_PATH", "migrations"),
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return i, nil
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return d, nil
}
