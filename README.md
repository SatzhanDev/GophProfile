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

## Запуск

```
cp .env.example .env      # при желании поправить под себя
docker compose up -d postgres minio
go mod tidy
go run ./cmd/server
```

При старте сервер подключается к PostgreSQL и применяет миграции из `migrations/`,
а также подключается к MinIO и создаёт бакет из `S3_BUCKET`, если его ещё нет.
Файл `.env` в корне проекта подхватывается автоматически (через `godotenv`) — можно
менять значения там, не трогая переменные окружения шелла. Если `.env` нет —
приложение не падает, а просто использует дефолты/переменные окружения процесса
(так и должно быть в докере/проде, где `.env`-файла обычно не бывает).

Проверка: `curl http://localhost:8080/health` — должно вернуться
`{"status":"ok","components":{"postgres":{"status":"ok"},"s3":{"status":"ok"}}}`.

Веб-консоль MinIO (посмотреть загруженные файлы глазами) — http://localhost:9001,
логин/пароль `minioadmin`/`minioadmin`.

## Переменные окружения

| Переменная               | По умолчанию  | Описание                     |
|---------------------------|---------------|-------------------------------|
| APP_ENV                   | development   | окружение приложения          |
| SERVER_HOST                | 0.0.0.0       | адрес, на котором слушает сервер |
| SERVER_PORT                | 8080          | порт HTTP-сервера              |
| SERVER_SHUTDOWN_TIMEOUT    | 10s           | таймаут graceful shutdown      |
| DB_HOST                    | localhost     | адрес PostgreSQL               |
| DB_PORT                    | 5434          | порт PostgreSQL (не 5432/5433 — чтобы не конфликтовать с уже запущенными локальными Postgres) |
| DB_USER                    | gophprofile   | пользователь PostgreSQL        |
| DB_PASSWORD                | gophprofile   | пароль PostgreSQL              |
| DB_NAME                    | gophprofile   | имя базы данных                |
| DB_SSLMODE                 | disable       | режим SSL для подключения      |
| S3_ENDPOINT                | localhost:9000| адрес S3-совместимого хранилища (без схемы http/https) |
| S3_ACCESS_KEY              | minioadmin    | access key                     |
| S3_SECRET_KEY              | minioadmin    | secret key                     |
| S3_BUCKET                  | avatars       | бакет, где хранятся файлы аватарок |
| S3_USE_SSL                 | false         | использовать ли HTTPS до хранилища |
| MIGRATIONS_PATH            | migrations    | путь к папке с SQL-миграциями  |

## План инкрементов

1. Скелет проекта, конфиг, `/health` — **готово**
2. Домен + PostgreSQL (миграции, репозиторий аватарок) — **готово**
3. Интеграция с S3/MinIO — **готово**
4. REST API: загрузка, получение, удаление аватарок
5. RabbitMQ: публикация событий после загрузки
6. Worker: генерация миниатюр, обработка удаления
7. Веб-интерфейс (загрузка + галерея)
8. Юнит-тесты, golangci-lint
9. Docker / docker-compose
10. Бонус: безопасность (magic bytes, rate limiting, CORS)
