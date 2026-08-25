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

// Config — корневая структура конфигурации всего приложения.
// В следующих инкрементах сюда добавятся секции Database, S3, Broker.
type Config struct {
	Env    string
	Server ServerConfig
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

	cfg := &Config{
		Env: getEnv("APP_ENV", "development"),
		Server: ServerConfig{
			Host:            getEnv("SERVER_HOST", "0.0.0.0"),
			Port:            port,
			ShutdownTimeout: shutdownTimeout,
		},
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
