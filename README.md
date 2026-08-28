# GophProfile

Микросервис для загрузки, хранения и раздачи аватарок пользователей по их идентификатору.

## Стек

Go, Chi, PostgreSQL, MinIO (S3), RabbitMQ, Docker.

## Структура проекта

```
cmd/server/     — точка входа HTTP-сервера
cmd/worker/     — точка входа воркера асинхронной обработки
internal/api/   — сборка роутера и middleware
internal/config/— загрузка конфигурации из env
internal/domain/    — доменные модели
internal/handlers/  — HTTP-обработчики
internal/repository/— доступ к PostgreSQL
internal/services/  — бизнес-логика
internal/worker/    — обработчики событий брокера
pkg/            — переиспользуемые пакеты общего назначения
web/            — веб-интерфейс: HTML-шаблоны, CSS, JS (вшиты в бинарник через go:embed)
migrations/     — SQL-миграции
docker/         — Dockerfile (multi-stage сборка server+worker)
k8s/            — манифесты Kubernetes
tests/          — интеграционные тесты
```

## Запуск

### Целиком в Docker (ближе всего к проду)

```
docker compose up --build
```

Одна команда поднимает всё: PostgreSQL, MinIO, RabbitMQ, `server` (порт 8080)
и `worker` — оба собираются из одного `docker/Dockerfile` (два бинарника в
одном образе, `worker`-сервис просто переопределяет команду запуска на
`./worker`). Оба применяют миграции при старте — специально сделано
идемпотентно и независимо друг от друга, потому что в докере нет гарантии,
какой из двух контейнеров реально стартует первым.

`docker-compose.yml` в этом режиме сам прописывает `server`/`worker`
переменные окружения (адреса `postgres`/`minio`/`rabbitmq` — это имена
сервисов, не `localhost`: внутри docker-сети контейнеры видят друг друга
по имени, а не по портам, проброшенным наружу на твой Мак).

Собрать без запуска: `docker compose build`. Пересобрать после правок в
коде: `docker compose up --build`. Посмотреть логи одного сервиса:
`docker compose logs -f worker`. Остановить всё и снести volumes (полная
очистка БД/S3/очереди): `docker compose down -v`.

### Гибридный режим (инфраструктура в Docker, Go — локально)

Удобнее для разработки: не нужно пересобирать образ на каждое изменение кода.

```
cp .env.example .env      # при желании поправить под себя
docker compose up -d postgres minio rabbitmq
go mod tidy
go run ./cmd/server
```

Воркер — отдельный процесс, запускается в соседнем терминале:

```
go run ./cmd/worker
```

(или `make run-worker`). Оба процесса читают одну и ту же конфигурацию
из `.env`/переменных окружения (для этого режима — с `localhost`-адресами
и нестандартными портами, см. таблицу ниже).

При старте сервер подключается к PostgreSQL и применяет миграции из `migrations/`,
подключается к MinIO и создаёт бакет из `S3_BUCKET`, если его ещё нет, а также
подключается к RabbitMQ и объявляет exchange `avatars.exchange`.
Файл `.env` в корне проекта подхватывается автоматически (через `godotenv`) — можно
менять значения там, не трогая переменные окружения шелла. Если `.env` нет —
приложение не падает, а просто использует дефолты/переменные окружения процесса
(так и должно быть в докере/проде, где `.env`-файла обычно не бывает).

Проверка: `curl http://localhost:8080/health` — должно вернуться
`{"status":"ok","components":{"postgres":{"status":"ok"},"s3":{"status":"ok"},"rabbitmq":{"status":"ok"}}}`.

Веб-консоли: MinIO — http://localhost:9001 (`minioadmin`/`minioadmin`),
RabbitMQ — http://localhost:15673 (`guest`/`guest`, вкладка Exchanges покажет
`avatars.exchange`).

## API аватарок

```
# Загрузить аватарку
curl -X POST http://localhost:8080/api/v1/avatars \
  -H "X-User-ID: user-1" \
  -F "file=@avatar.jpg"

# Получить аватарку по её id (бинарные данные)
curl http://localhost:8080/api/v1/avatars/<avatar_id> -o out.jpg

# Получить самую свежую аватарку пользователя (заглушка, если её ещё нет)
curl http://localhost:8080/api/v1/users/user-1/avatar -o out.png

# Метаданные аватарки
curl http://localhost:8080/api/v1/avatars/<avatar_id>/metadata

# Все аватарки пользователя
curl http://localhost:8080/api/v1/users/user-1/avatars

# Удалить аватарку (может только владелец — сверяется X-User-ID)
curl -X DELETE http://localhost:8080/api/v1/avatars/<avatar_id> -H "X-User-ID: user-1"
```

После каждой успешной загрузки и удаления сервер публикует событие в
`avatars.exchange` (routing key `avatar.uploaded`/`avatar.deleted`). Воркер
(запущенный отдельно, см. выше) их разбирает: на `avatar.uploaded` — качает
оригинал, режет миниатюры 100x100 и 300x300 (всегда в JPEG, независимо от
формата оригинала) и кладёт их в S3; на `avatar.deleted` — стирает из S3
оригинал и все миниатюры. Через несколько секунд после загрузки
`GET /avatars/{id}/metadata` покажет непустой `thumbnails`, а
`GET /avatars/{id}?size=100x100` отдаст готовую миниатюру.

Проверить, что события долетают и разбираются, можно в веб-консоли
RabbitMQ (http://localhost:15673) — на вкладке Queues у `avatars.thumbnails`
и `avatars.cleanup` счётчик `Ready` должен обнуляться почти сразу после
публикации, если воркер запущен и работает.

## Веб-интерфейс

Открой в браузере http://localhost:8080/web/upload — форма загрузки с
drag&drop, превью картинки до отправки и прогресс-баром. После успешной
загрузки — редирект на `/web/gallery/{user_id}` со списком аватарок
пользователя и кнопкой удаления у каждой.

Форма рабочая даже без JavaScript (обычный `<form method="POST">` на
`/web/upload`) — JS в `web/static/upload.js` лишь прогрессивно улучшает
её (прогресс-бар, без перезагрузки страницы). А вот удаление в галерее
требует JS: HTML-формы не умеют отправлять `DELETE` и произвольные
заголовки (`X-User-ID`), которых требует наш REST API.

## Тесты и линтер

`golangci-lint` — отдельная утилита, не часть стандартного `go`, ставится один раз:
```
brew install golangci-lint
# либо, если brew не хочется:
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

```
go mod tidy       # подтянет testify/testcontainers-go, если ещё не подтянул
make test         # юнит-тесты всех пакетов, без Docker и сети
make cover        # процент покрытия по пакетам (после make test)
make lint         # golangci-lint (go vet, staticcheck, errcheck и т.д.)
```

Юнит-тесты лежат рядом с кодом (`*_test.go` в `internal/...` и `pkg/...`) и
проверяют логику через фейки — например, `internal/services/avatar_service_test.go`
подставляет вместо реальных PostgreSQL/S3/RabbitMQ маленькие in-memory
реализации тех же интерфейсов (`Storage`, `EventPublisher`, `repository.AvatarRepository`).
Это стало возможным именно благодаря тому, что с самого начала эти
зависимости передавались как интерфейсы, а не конкретные типы.

`internal/repository/avatar_repository_test.go` — отдельный случай: сам
`PostgresAvatarRepository` не подменить фейком целиком (это и есть
реализация), поэтому здесь подменяется пул соединений через `pgxmock` —
библиотеку, которая подделывает `pgx` на уровне запросов/строк, без
реального PostgreSQL. Ради этого `PostgresAvatarRepository` стал зависеть
от узкого интерфейса `pgxPool` (`QueryRow`/`Query`/`Exec`), а не от
конкретного `*pgxpool.Pool` — тот же приём, что и везде в проекте.

Отдельно — интеграционный тест на настоящем PostgreSQL в Docker-контейнере
(через testcontainers-go и testify/suite), он лежит в `tests/integration/`
и помечен build tag `integration`, поэтому не запускается вместе с
обычными тестами:

```
make test-integration
```

Требует запущенный Docker — testcontainers сам поднимет и потом уничтожит
контейнер с Postgres на время теста.

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
| RABBITMQ_URL               | amqp://guest:guest@localhost:5673/ | адрес RabbitMQ (AMQP) |
| CORS_ALLOWED_ORIGINS       | *             | разрешённые origin'ы через запятую |
| RATE_LIMIT_RPS             | 5             | лимит запросов в секунду на один IP (API) |
| RATE_LIMIT_BURST           | 10            | сколько запросов можно сделать одним всплеском |
| MIGRATIONS_PATH            | migrations    | путь к папке с SQL-миграциями  |

## Безопасность

- **Magic bytes вместо расширения/заголовка.** `POST /api/v1/avatars` определяет
  реальный тип файла по первым байтам содержимого (`http.DetectContentType`),
  а не по имени файла или `Content-Type` от клиента — файл `virus.exe`,
  переименованный в `photo.jpg`, не пройдёт (см. инкремент 4).
- **Лимит размера файла** — 10 МБ, обрезается ещё до полного чтения тела
  запроса (`http.MaxBytesReader`), сервер не станет буферизовать гигабайтный
  файл только для того, чтобы потом отказать.
- **Валидация `X-User-ID`/`user_id`** (`internal/handlers/validate.go`) —
  формат (буквы/цифры/`.`/`_`/`@`/`+`/`-`) и длина до 255 символов (предел
  колонки в БД). Проверяется везде, где идентификатор пользователя влияет
  на запись данных: загрузка и оба варианта удаления, включая HTML-форму.
- **Rate limiting** (`internal/api/ratelimit.go`) — token bucket на IP,
  свой лимит на каждого клиента, применяется ко всей группе `/api/v1`.
  Реализовано вручную на `golang.org/x/time/rate`, без сторонних
  rate-limiting-библиотек — так виден сам алгоритм, а не только вызов
  готовой функции.
- **CORS** (`github.com/go-chi/cors`) — список разрешённых origin'ов
  настраивается через `CORS_ALLOWED_ORIGINS`, по умолчанию `*` (разработка).

## План инкрементов

1. Скелет проекта, конфиг, `/health` — **готово**
2. Домен + PostgreSQL (миграции, репозиторий аватарок) — **готово**
3. Интеграция с S3/MinIO — **готово**
4. REST API: загрузка, получение, удаление аватарок — **готово**
5. RabbitMQ: публикация событий после загрузки — **готово**
6. Worker: генерация миниатюр, обработка удаления — **готово**
7. Веб-интерфейс (загрузка + галерея) — **готово**
8. Юнит-тесты, golangci-lint — **готово**
9. Docker / docker-compose — **готово**
10. Бонус: безопасность (magic bytes, rate limiting, CORS) — **готово**
