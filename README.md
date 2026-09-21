# GophProfile

Микросервис для загрузки, хранения и раздачи аватарок пользователей по их идентификатору.

## Стек

Go, Chi, PostgreSQL, MinIO (S3), RabbitMQ, Docker, OpenTelemetry + Jaeger, Prometheus + Alertmanager, Grafana + Loki + Promtail, Kubernetes + Helm.

## Структура проекта

```
cmd/server/     — точка входа HTTP-сервера
cmd/worker/     — точка входа воркера асинхронной обработки
cmd/migrate/    — точка входа для разового применения миграций (Helm-хук)
internal/api/   — сборка роутера и middleware
internal/config/— загрузка конфигурации из env
internal/domain/    — доменные модели
internal/handlers/  — HTTP-обработчики
internal/repository/— доступ к PostgreSQL
internal/services/  — бизнес-логика
internal/worker/    — обработчики событий брокера
pkg/telemetry/  — трейсинг (OpenTelemetry → Jaeger) и корреляция логов
pkg/metrics/    — Prometheus-метрики (RED, бизнес-, инфраструктурные) и HTTP-middleware
pkg/            — переиспользуемые пакеты общего назначения
web/            — веб-интерфейс: HTML-шаблоны, CSS, JS (вшиты в бинарник через go:embed)
migrations/     — SQL-миграции
docker/         — Dockerfile (multi-stage сборка server+worker+migrate), конфиги Prometheus/Alertmanager/Promtail/Grafana
k8s/            — Helm-чарт (Chart.yaml, values*.yaml, templates/) для деплоя в Kubernetes
tests/          — интеграционные тесты
openapi.yaml    — спецификация REST API (OpenAPI 3.0)
```

## Архитектура

```mermaid
graph TD
    User["Внешний клиент<br/>браузер / сторонняя платформа"]

    subgraph K8S["Kubernetes-кластер (Helm-релиз gophprofile)"]
        Ingress["Ingress (nginx)"]
        SvcServer["Service: server"]
        SvcWorker["Service: worker<br/>(:9100/metrics)"]
        DeployServer["Deployment: server<br/>HPA 2..10 реплик"]
        DeployWorker["Deployment: worker<br/>HPA 1..5 реплик"]
        CM["ConfigMap"]
        Secret["Secret"]
        SM["ServiceMonitor"]
        Job["Job: migrate<br/>(pre-upgrade hook)"]
    end

    subgraph DATA["Данные и очередь (внешние зависимости чарта)"]
        PG[("PostgreSQL")]
        S3[("MinIO / S3")]
        MQ{{"RabbitMQ"}}
    end

    subgraph OBS["Observability"]
        Jaeger["Jaeger"]
        Prom["Prometheus"]
        AM["Alertmanager"]
        Loki["Loki"]
        Promtail["Promtail"]
        Grafana["Grafana"]
    end

    User --> Ingress --> SvcServer --> DeployServer
    SvcWorker --> DeployWorker
    SM -. "scrape /metrics" .-> SvcServer
    SM -. "scrape /metrics" .-> SvcWorker

    CM --> DeployServer
    CM --> DeployWorker
    Secret --> DeployServer
    Secret --> DeployWorker
    Job -. "миграции перед раскаткой" .-> PG

    DeployServer --> PG
    DeployServer --> S3
    DeployServer -- "publish avatar.uploaded/deleted" --> MQ
    MQ -- "consume" --> DeployWorker
    DeployWorker --> PG
    DeployWorker --> S3

    DeployServer -. "OTLP" .-> Jaeger
    DeployWorker -. "OTLP" .-> Jaeger
    Prom --> SM
    Prom --> AM
    DeployServer -. "stdout logs" .-> Promtail
    DeployWorker -. "stdout logs" .-> Promtail
    Promtail --> Loki
    Grafana --> Prom
    Grafana --> Loki
    Grafana --> Jaeger
```

`PostgreSQL`/`MinIO`/`RabbitMQ` показаны вне блока Kubernetes-кластера
намеренно: Helm-чарт в `k8s/` разворачивает только само приложение
GophProfile (server + worker и всё вокруг них), а не полный стек
зависимостей — см. подробное объяснение в `k8s/values.yaml`
(комментарий у секции `config`) и раздел «В Kubernetes через Helm» ниже.

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

Прогнать миграции отдельно, не поднимая сервер целиком (тот же бинарник,
которым в Kubernetes управляет Helm-хук — см. `cmd/migrate` и раздел
про Kubernetes ниже): `docker compose --profile jobs run --rm migrate`.

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

### В Kubernetes через Helm (Rancher Desktop)

Чарт лежит в `k8s/` и деплоит только само приложение (server + worker) —
PostgreSQL/MinIO/RabbitMQ он не поднимает (см. «Архитектура» выше). Проще
всего для локальной проверки — оставить их в docker-compose и подружить
сеть Rancher Desktop с сетью docker-compose, либо поднять их же вручную
внутри кластера. Дальше — сам деплой приложения:

```
# 1. Собрать образ в тот же Docker-демон, которым пользуется Rancher Desktop
docker build -t gophprofile:local -f docker/Dockerfile .

# 2. Проверить чарт локально, без применения к кластеру
make helm-lint
make helm-template     # посмотреть, что реально сгенерируется

# 3. Установить релиз с дев-оверрайдами (k8s/values-dev.yaml)
helm install gophprofile ./k8s -f k8s/values-dev.yaml \
  --set config.dbHost=host.docker.internal \
  --set config.s3Endpoint=host.docker.internal:9000 \
  --set secrets.rabbitmqURL="amqp://guest:guest@host.docker.internal:5673/"
```

`host.docker.internal` — то, как контейнеры внутри Rancher Desktop обычно
достают до сервисов, поднятых на хосте через docker-compose (порты 5434/9000/5673
из `.env.example`, а не 5432/9000/5672 внутренние — то же самое различие,
что и в гибридном режиме запуска выше). Если вместо этого поднимаешь
Postgres/MinIO/RabbitMQ прямо в кластере — подставь их реальные Service-адреса.

Дальше — как с любым Helm-релизом:

```
kubectl get pods                       # server/worker должны стать Running
kubectl logs -l app.kubernetes.io/component=worker -f
helm upgrade gophprofile ./k8s -f k8s/values-dev.yaml   # применить изменения (включая Job миграций)
helm uninstall gophprofile             # снести релиз целиком
```

Для доступа через Ingress — добавь запись в `/etc/hosts`
(`127.0.0.1 gophprofile.local`) и открой `http://gophprofile.local/web/upload`;
без Ingress — `kubectl port-forward svc/gophprofile-server 8080:80`.

Что внутри чарта (подробные комментарии — прямо в файлах `k8s/templates/`):
- **Deployment + Service + Ingress + ConfigMap + Secret** — базовые ресурсы
  приложения. `Secret` создаётся через `stringData` (Kubernetes сам считает
  base64), либо `secrets.create: false`, если секрет заводится снаружи
  (CI/CD, Vault, Sealed Secrets) — см. `k8s/values-prod.yaml`.
- **HorizontalPodAutoscaler** — по CPU/RAM отдельно для server и worker
  (`k8s/templates/hpa-*.yaml`); требует `metrics-server` в кластере и
  заданные `resources.requests` у контейнера — без этого HPA просто не считает утилизацию.
- **ServiceMonitor** (`k8s/templates/servicemonitor.yaml`) — выключен по
  умолчанию (`serviceMonitor.enabled: false`): это CRD от Prometheus
  Operator, которого нет в ванильном кластере. Включай только если в
  кластере уже стоит kube-prometheus-stack или аналог.
- **NetworkPolicy** — по умолчанию включена в проде, выключена в
  `values-dev.yaml` (на локальном кластере обычно нет ни
  `ingress-nginx`, ни `monitoring` неймспейсов с нужными лейблами —
  включённая политика просто заблокирует весь трафик наглухо).
- **ServiceAccount без токена** (`automountServiceAccountToken: false`) +
  **SecurityContext** (non-root UID 65532, `readOnlyRootFilesystem`,
  `drop: ["ALL"]` капабилити) — приложению ни разу не нужен доступ к
  Kubernetes API, поэтому токен ему просто не выдаётся.
- **Job миграций** (`k8s/templates/job-migrate.yaml`) — Helm
  `pre-upgrade`-хук, гоняет отдельный бинарник `cmd/migrate`. Сознательно
  не `pre-install` — на первой установке ConfigMap/Secret ещё не
  существуют (Helm создаёт хуки до обычных ресурсов чарта), а
  `server`/`worker` и так безопасно накатывают миграции сами при первом
  старте.
- **Graceful shutdown** — `server`/`worker` уже умели корректно
  останавливаться сами (`signal.NotifyContext` + `srv.Shutdown` с таймаутом,
  см. `cmd/server/main.go`), но в Kubernetes к этому добавляются ещё два
  элемента. `terminationGracePeriodSeconds` (30с у server, 45с у worker —
  ему нужно чуть больше, чтобы дошить текущую генерацию миниатюр, а не
  оборвать её на середине) — сколько всего времени Kubernetes ждёт перед
  тем, как убить процесс принудительно (SIGKILL) после SIGTERM.
  `preStop`-хук у server (`sleep 5`) решает отдельную гонку: kubelet
  посылает SIGTERM и одновременно начинает убирать под из Endpoints
  Service'а, но эти два события не синхронизированы — часть кластера
  может продолжать слать трафик на под ещё секунду-другую после SIGTERM,
  пока обновление не разошлось по всем `kube-proxy`. У worker такого
  хука нет — на него не льётся живой пользовательский трафик через
  Service (только периодический скрейп метрик), терять который не жалко.

## API аватарок

Полная и актуальная спецификация — в `openapi.yaml` (OpenAPI 3.0, корень
репозитория). Посмотреть её как интерактивную страницу, не поднимая
ничего своего: открой https://editor.swagger.io и вставь содержимое
файла, либо `npx @redocly/cli preview-docs openapi.yaml` локально. Ниже —
только самые ходовые запросы для быстрой проверки руками.

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
| DB_PASSWORD                | *(обязателен, дефолта нет)* | пароль PostgreSQL |
| DB_NAME                    | gophprofile   | имя базы данных                |
| DB_SSLMODE                 | disable       | режим SSL для подключения      |
| S3_ENDPOINT                | localhost:9000| адрес S3-совместимого хранилища (без схемы http/https) |
| S3_ACCESS_KEY              | *(обязателен, дефолта нет)* | access key       |
| S3_SECRET_KEY              | *(обязателен, дефолта нет)* | secret key       |
| S3_BUCKET                  | avatars       | бакет, где хранятся файлы аватарок |
| S3_USE_SSL                 | false         | использовать ли HTTPS до хранилища |
| RABBITMQ_URL               | *(обязателен, дефолта нет)* | адрес RabbitMQ (AMQP) |
| CORS_ALLOWED_ORIGINS       | *             | разрешённые origin'ы через запятую |
| RATE_LIMIT_RPS             | 5             | лимит запросов в секунду на один IP (API) |
| RATE_LIMIT_BURST           | 10            | сколько запросов можно сделать одним всплеском |
| MIGRATIONS_PATH            | migrations    | путь к папке с SQL-миграциями  |
| OTEL_SERVICE_NAME          | *(своё для server/worker)* | имя сервиса в Jaeger |
| OTEL_EXPORTER_OTLP_ENDPOINT| localhost:4317| адрес Jaeger по протоколу OTLP/gRPC |
| METRICS_PORT               | 9100          | порт отдельного HTTP-сервера с `/metrics` у worker (у server — на SERVER_PORT) |

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
- **Никаких дефолтов для чувствительных параметров.** `DB_PASSWORD`,
  `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `RABBITMQ_URL` не имеют значений по
  умолчанию в коде — раньше там были общеизвестные дефолты вроде
  `minioadmin`/`guest:guest`, и если забыть задать их явно при деплое,
  сервис незаметно стартовал бы с небезопасными реквизитами. Теперь
  вместо этого `config.Load()` возвращает ошибку сразу при старте
  (fail fast), если хоть один из них не задан. Для локальной разработки
  ничего не меняется — значения по-прежнему берутся из `.env`.
- **CORS** (`github.com/go-chi/cors`) — список разрешённых origin'ов
  настраивается через `CORS_ALLOWED_ORIGINS`, по умолчанию `*` (разработка).

## Трейсинг (OpenTelemetry → Jaeger)

```
docker compose up -d jaeger   # если работаешь в гибридном режиме
```
Открой http://localhost:16686 (Jaeger UI), выбери сервис `gophprofile-server`
или `gophprofile-worker` и посмотри трейсы.

Что покрыто (`pkg/telemetry/`):
- **HTTP** — весь роутер обёрнут в `otelhttp.NewHandler` (`cmd/server/main.go`):
  span на каждый запрос, с извлечением `traceparent` из входящих заголовков.
- **PostgreSQL** — `telemetry.PgxTracer` подключается к пулу один раз
  в `pkg/postgres.NewPool` и автоматически оборачивает в span **каждый**
  SQL-запрос — ни один метод репозитория трогать не пришлось.
- **S3** — ручные спаны вокруг `Upload`/`Download`/`Delete` в `pkg/s3`.
- **RabbitMQ + распространение контекста между процессами** — при публикации
  (`pkg/broker.Publisher.Publish`) контекст трейсинга внедряется прямо
  в заголовки AMQP-сообщения; воркер (`internal/worker.RunLoop`) достаёт его
  оттуда и продолжает **тот же трейс**. Поэтому путь "HTTP-запрос → публикация
  события → обработка в отдельном процессе-воркере" виден в Jaeger одной
  цепочкой, хотя это два независимых процесса.
- **Бизнес-логика** — `AvatarService.Upload` оборачивается в span
  `upload_avatar` с атрибутами `user_id`/`file_name`/`file_size`/`avatar_id`
  (пример ровно по ТЗ).
- **Корреляция логов с трейсами** — `telemetry.TraceHandler` оборачивает
  `slog`-хендлер и на каждый вызов `slog.*Context(ctx, ...)` добавляет
  `trace_id`/`span_id` в JSON лога. Увидев ошибку в логах — можно по
  `trace_id` открыть в Jaeger весь путь запроса, который к ней привёл.

## Метрики (Prometheus)

```
docker compose up -d prometheus   # поднимет заодно server и worker, если их ещё нет
```
Открой http://localhost:9090 (Prometheus UI) → вкладка Status → Targets — там
должны быть `gophprofile-server` (server:8080/metrics) и `gophprofile-worker`
(worker:9100/metrics) в состоянии `UP`. Метрики можно и просто посмотреть
глазами: `curl localhost:8080/metrics` (сервер) или `curl localhost:9100/metrics` (воркер).

Что собирается (`pkg/metrics/`):
- **HTTP RED-метрики** (Rate/Errors/Duration) — `gophprofile_http_requests_total`,
  `gophprofile_http_request_duration_seconds`, `gophprofile_http_requests_in_flight`.
  Пишутся `metrics.HTTPMiddleware` (`pkg/metrics/http_middleware.go`), подключённым
  только на группу `/api/v1` в `internal/api/router.go` — по той же логике,
  что и rate limiter: это трафик от внешних клиентов, именно его Rate/Errors/Duration
  интересны для дашборда. Метка `route` — это шаблон маршрута
  (`/api/v1/avatars/{avatarID}`), а не фактический путь с UUID — иначе у каждой
  аватарки была бы своя метка и метрика "взорвалась" бы по количеству уникальных значений.
- **Бизнес-метрики** — `gophprofile_avatar_uploads_total`,
  `gophprofile_avatar_upload_duration_seconds`, `gophprofile_avatar_deletes_total`,
  `gophprofile_avatar_storage_bytes` (растёт/уменьшается на размер оригинала при
  загрузке/удалении — оценка приблизительная, миниатюры не учитываются). Пишутся
  прямо в `internal/services/avatar_service.go`.
- **Инфраструктурные метрики** — `gophprofile_db_pool_connections` (idle/used/max,
  снимается раз в 15с из `pgxpool.Stat()` — `metrics.CollectDBPoolStats`, запущен
  и в `cmd/server`, и в `cmd/worker`), `gophprofile_worker_jobs_total` и
  `gophprofile_worker_job_duration_seconds` (по очереди upload/delete, пишутся
  в `internal/worker/runner.go`).
- У воркера нет своего HTTP-API, поэтому под `/metrics` у него отдельный
  лёгкий HTTP-сервер на порту `METRICS_PORT` (по умолчанию 9100) —
  см. `cmd/worker/main.go`.

Пример PromQL-запросов, которые можно вбить в Prometheus UI (Graph):
```
rate(gophprofile_http_requests_total{status=~"5.."}[5m])   # ошибки в секунду
histogram_quantile(0.95, rate(gophprofile_http_request_duration_seconds_bucket[5m]))  # p95 задержки
gophprofile_avatar_storage_bytes                            # текущий объём хранилища
```

## Логирование (Grafana Loki + Promtail)

```
docker compose up -d loki promtail grafana
```
Открой http://localhost:3000 (Grafana, анонимный вход с правами Admin — см.
`GF_AUTH_ANONYMOUS_*` в docker-compose.yml) → Explore → источник данных Loki →
запрос `{service="server"}` — увидишь JSON-логи сервера в реальном времени.

Как это устроено — в отличие от трейсинга и метрик, здесь **не понадобилось
менять ни строчки Go-кода**: структурные JSON-логи с `trace_id`/`span_id`
уже писались в stdout с самого первого инкремента наблюдаемости
(`pkg/telemetry/logging.go`), а Docker сам сохраняет stdout любого контейнера
в файл на диске. Дальше — чисто инфраструктурная часть:

- **Promtail** (`docker/promtail.yml`) через Docker API (`docker_sd_configs`)
  узнаёт, какие контейнеры вообще запущены, находит их лог-файлы на диске
  (`/var/lib/docker/containers/<id>/*-json.log`) и построчно отправляет
  их в Loki. Лейблы `container`/`service` берутся из имени и
  compose-лейблов контейнера (`service` — это имя сервиса из
  docker-compose.yml: `server`, `worker`, `postgres` и т.д., а не OTEL-имя
  из Jaeger) — это то, по чему потом фильтруют в Grafana (`{service="worker"}`).
- Стадия пайплайна `docker: {}` снимает "конверт" самого Docker
  (`{"log": "...", "stream": "stdout", "time": "..."}`), а следующая
  `json:` уже разбирает наш собственный JSON от `slog` и достаёт из него
  `level` — он превращается в лейбл Loki (`| level="error"`), у него всего
  4 возможных значения, это дёшево.
- **`trace_id` лейблом не становится** — сознательно. Лейблы в Loki
  устроены так же, как в Prometheus: каждое новое значение заводит
  отдельный "стрим" на диске, а `trace_id` уникален на каждый запрос —
  сделай его лейблом, и получится ровно тот cardinality explosion, который
  уже разбирали для Prometheus и UUID в путях (`pkg/metrics/http_middleware.go`).
  Найти логи конкретного трейса всё равно можно — просто фильтром на
  момент запроса, а не заранее проиндексированным лейблом:
  ```
  {service="server"} | json | trace_id="<id из Jaeger>"
  ```
  Это и есть обещанная в трейсинге связка: открыл трейс в Jaeger →
  скопировал `trace_id` → нашёл по нему все логи в Grafana.
- **Grafana** и её datasource'ы (Prometheus/Loki/Jaeger) провижинятся файлом
  `docker/grafana/provisioning/datasources/datasources.yml` при старте,
  без единого клика в UI. Готовые дашборды — см. следующий раздел.

## Дашборды (Grafana)

Открой `http://localhost:3000` → папка **GophProfile** в левом меню (Dashboards).
Три дашборда, провижинятся файлами из `docker/grafana/provisioning/dashboards/`
(`provider.yml` — конфиг провайдера, остальные `*.json` — сами дашборды; Grafana
перечитывает эту папку раз в 30 секунд, менять можно без пересоздания контейнера):

- **GophProfile — HTTP (RED)** (`http-red.json`) — Rate/Errors/Duration
  по `gophprofile_http_*`: запросов в секунду по маршруту, доля 5xx,
  перцентили p50/p95/p99 задержки, текущее число запросов в обработке.
- **GophProfile — Ресурсы** (`resources.json`) — CPU и память процессов
  `server`/`worker` (стандартные метрики Go-рантайма `process_*`, их
  бесплатно добавляет `promhttp.Handler()` — мы их никак специально не
  писали), состояние пула соединений с PostgreSQL, открытые файловые дескрипторы.
- **GophProfile — Бизнес-метрики** (`business.json`) — загрузки и удаления
  аватарок в секунду по итогу (success/validation_error/storage_error),
  текущая оценка объёма хранилища, обработка событий воркером
  (по очереди upload/delete) и её задержка.

Технический момент: в `docker/grafana/provisioning/datasources/datasources.yml`
у каждого источника данных явно зафиксирован `uid` (`prometheus`/`loki`/`jaeger`).
Без этого Grafana сгенерировала бы `uid` случайно при первом старте, и ссылки
`"datasource": {"uid": "prometheus"}` в уже написанных JSON-дашбордах никого
бы не находили — панели были бы "битыми". Явный `uid` — единственный способ
сослаться на источник данных из заранее подготовленного файла дашборда.

## Алертинг (Prometheus Alertmanager) — бонус

```
docker compose up -d alertmanager prometheus
```
Открой `http://localhost:9093` (Alertmanager UI) — там видны все активные
алерты. В Prometheus (`http://localhost:9090` → Alerts) видно то же самое,
но ещё и с текущим значением условия у каждого правила — удобно для
отладки, почему алерт ещё не сработал.

Как это устроено: Prometheus (`docker/rules.yml`) каждые 15 секунд
пересчитывает PromQL-условие каждого правила; если оно остаётся истинным
дольше `for` — правило переходит в firing и уходит в Alertmanager
(`docker/prometheus.yml`, секции `rule_files`/`alerting`). Alertmanager
(`docker/alertmanager.yml`) группирует похожие алерты в одно уведомление,
не спамит повторно чаще `repeat_interval` и решает, кому это отправить.

Правила (`docker/rules.yml`):
- `GophProfileServerDown` / `GophProfileWorkerDown` — `up == 0` дольше
  минуты (сама метрика `up` — служебная, её добавляет Prometheus на
  каждую цель из `scrape_configs`).
- `HighHTTPErrorRate` — доля 5xx среди ответов API за 5 минут выше 5%.
- `HighHTTPLatencyP95` — p95 задержки ответа API выше 1 секунды 5 минут подряд.
- `DBPoolNearlyExhausted` — больше 80% соединений пула Postgres заняты
  дольше 5 минут (тут нужен `on(job)` в PromQL — делим две метрики
  с разными значениями лейбла `state`, поэтому явно указываем, по какому
  общему лейблу их сопоставлять).
- `WorkerJobFailures` — воркер продолжает получать ошибки уже после
  исчерпания retry (см. `internal/worker/runner.go`) — разовый сбой
  так не поймать, а вот системную проблему с очередью — да.

Получатель уведомлений (`receivers` в `docker/alertmanager.yml`) сейчас
пустой — без реальной интеграции в Slack/email/Telegram: это учебный
проект, заводить для него настоящий канал уведомлений незачем. Алерты всё
равно видны в UI Alertmanager независимо от того, настроен ли получатель —
этого достаточно, чтобы убедиться, что правила реально работают. В проде
в `receivers` добавляется `slack_configs`/`email_configs`/`webhook_configs`.

Проверить, что алерт реально стреляет, проще всего на `GophProfileWorkerDown`:
```
docker compose stop worker
```
подожди больше минуты и обнови `http://localhost:9093` — алерт должен
появиться в состоянии `firing`. Не забудь вернуть воркер обратно:
```
docker compose start worker
```

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

### Спринт 2: наблюдаемость

1. Трейсинг: OpenTelemetry (HTTP, БД, S3, брокер, распространение контекста) + Jaeger, корреляция логов — **готово**
2. Метрики: Prometheus (RED-метрики, бизнес- и инфраструктурные метрики) — **готово**
3. Логирование: Grafana Loki + Promtail — **готово**
4. Дашборды в Grafana — **готово**
5. Бонус: алертинг через Prometheus Alertmanager — **готово**

### Спринт 3: Kubernetes и Helm

1. Базовые манифесты: Deployment, Service, Ingress, ConfigMap, Secret — **готово**
2. Масштабируемость: HPA по CPU/RAM, liveness/readiness-пробы — **готово**
3. Мониторинг в кластере: ServiceMonitor для Prometheus Operator — **готово**
4. Безопасность: NetworkPolicy, ServiceAccount без токена, SecurityContext (non-root, read-only rootfs) — **готово**
5. Упаковка в Helm Chart: values для окружений, хук миграций БД (`cmd/migrate`) — **готово**
6. Продакшен: graceful shutdown в Kubernetes (preStop, terminationGracePeriodSeconds), актуальный OpenAPI, диаграмма архитектуры — **готово**
