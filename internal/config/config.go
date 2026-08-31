// Package config отвечает за загрузку настроек приложения из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

// CORSConfig содержит список origin'ов, которым разрешены cross-origin
// запросы к API (например, домен фронтенда, если он живёт отдельно от
// GophProfile). "*" — разрешить всем, разумный дефолт для разработки,
// но не для прода.
type CORSConfig struct {
	AllowedOrigins []string
}

// RateLimitConfig задаёт лимит запросов на один IP (алгоритм token bucket).
type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
}

// Config — корневая структура конфигурации всего приложения.
type Config struct {
	Env            string
	Server         ServerConfig
	Database       DatabaseConfig
	S3             S3Config
	Broker         BrokerConfig
	CORS           CORSConfig
	RateLimit      RateLimitConfig
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

	rateLimitRPS, err := getEnvFloat("RATE_LIMIT_RPS", 5)
	if err != nil {
		return nil, err
	}

	rateLimitBurst, err := getEnvInt("RATE_LIMIT_BURST", 10)
	if err != nil {
		return nil, err
	}

	// Для этих четырёх параметров дефолтов намеренно нет: это учётные
	// данные, и их дефолты ("gophprofile", "minioadmin", "guest:guest")
	// — общеизвестные значения по умолчанию у Postgres/MinIO/RabbitMQ.
	// Если их не задать явно при деплое (в докере/проде), сервис молча
	// стартовал бы с небезопасными реквизитами. Лучше упасть сразу и
	// явно на старте (fail fast), чем незаметно оказаться уязвимым.
	// Для локальной разработки этого не нужно бояться — значения
	// задаются в .env (см. .env.example), который подхватывается сам.
	dbPassword, err := getEnvRequired("DB_PASSWORD")
	if err != nil {
		return nil, err
	}

	s3AccessKey, err := getEnvRequired("S3_ACCESS_KEY")
	if err != nil {
		return nil, err
	}

	s3SecretKey, err := getEnvRequired("S3_SECRET_KEY")
	if err != nil {
		return nil, err
	}

	rabbitmqURL, err := getEnvRequired("RABBITMQ_URL")
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
			Password: dbPassword,
			Name:     getEnv("DB_NAME", "gophprofile"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		S3: S3Config{
			Endpoint:  getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey: s3AccessKey,
			SecretKey: s3SecretKey,
			Bucket:    getEnv("S3_BUCKET", "avatars"),
			UseSSL:    s3UseSSL,
		},
		Broker: BrokerConfig{
			URL: rabbitmqURL,
		},
		CORS: CORSConfig{
			AllowedOrigins: getEnvStringSlice("CORS_ALLOWED_ORIGINS", []string{"*"}),
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: rateLimitRPS,
			Burst:             rateLimitBurst,
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

// getEnvRequired — как getEnv, но без дефолта: для чувствительных
// параметров (пароли, ключи доступа), у которых не должно быть
// "безопасного значения по умолчанию" в принципе.
func getEnvRequired(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return v, nil
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

func getEnvFloat(key string, fallback float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %w", key, err)
	}
	return f, nil
}

// getEnvStringSlice читает список значений через запятую, например
// "https://a.com,https://b.com" -> []string{"https://a.com", "https://b.com"}.
func getEnvStringSlice(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}

	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
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
