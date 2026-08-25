# GophProfile

Микросервис для загрузки, хранения и раздачи аватарок пользователей по их идентификатору.

## Стек

Go, Chi, PostgreSQL, MinIO (S3), RabbitMQ, Docker.

## Структура проекта

```
cmd/server/     — точка входа HTTP-сервера
cmd/worker/     — точка входа воркера асинхронной обработки (появится позже)
internal/api/   — сборка роутера и middleware
internal/config/— загрузка конфигурации из env
internal/domain/    — доменные модели
internal/handlers/  — HTTP-обработчики
internal/repository/— доступ к PostgreSQL
internal/services/  — бизнес-логика
internal/worker/    — обработчики событий брокера
pkg/            — переиспользуемые пакеты общего назначения
web/            — фронтенд (SPA)
migrations/     — SQL-миграции
docker/         — Dockerfile и docker-compose
k8s/            — манифесты Kubernetes
tests/          — интеграционные тесты
```

## Запуск (инкремент 1)

```
go mod tidy
go run ./cmd/server
```

Проверка: `curl http://localhost:8080/health`

## Переменные окружения

| Переменная               | По умолчанию  | Описание                     |
|---------------------------|---------------|-------------------------------|
| APP_ENV                   | development   | окружение приложения          |
| SERVER_HOST                | 0.0.0.0       | адрес, на котором слушает сервер |
| SERVER_PORT                | 8080          | порт HTTP-сервера              |
| SERVER_SHUTDOWN_TIMEOUT    | 10s           | таймаут graceful shutdown      |

## План инкрементов

1. Скелет проекта, конфиг, `/health` — **готово**
2. Домен + PostgreSQL (миграции, репозиторий аватарок)
3. Интеграция с S3/MinIO
4. REST API: загрузка, получение, удаление аватарок
5. RabbitMQ: публикация событий после загрузки
6. Worker: генерация миниатюр, обработка удаления
7. Веб-интерфейс (загрузка + галерея)
8. Юнит-тесты, golangci-lint
9. Docker / docker-compose
10. Бонус: безопасность (magic bytes, rate limiting, CORS)
