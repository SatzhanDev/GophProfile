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

// S3Config содержит настройки подключения к S3-совместимому хранилищу (MinIO).
type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// BrokerConfig содержит настройки подключения к RabbitMQ.
type BrokerConfig struct {
	URL string
}

// Config — корневая структура конфигурации всего приложения.
type Config struct {
	Env            string
	Server         ServerConfig
	Database       DatabaseConfig
	S3             S3Config
	Broker         BrokerConfig
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

	s3UseSSL, err := getEnvBool("S3_USE_SSL", false)
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
		S3: S3Config{
			Endpoint:  getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("S3_SECRET_KEY", "minioadmin"),
			Bucket:    getEnv("S3_BUCKET", "avatars"),
			UseSSL:    s3UseSSL,
		},
		Broker: BrokerConfig{
			URL: getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5673/"),
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

func getEnvBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return b, nil
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
