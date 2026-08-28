# ---- build stage ----
# Собираем оба бинарника (server и worker) в одном образе — это ровно
# один Go-модуль с двумя точками входа (cmd/server, cmd/worker),
# нет смысла держать для них два отдельных Dockerfile.
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Кэш-трюк Docker: слой с зависимостями пересобирается только если
# менялись go.mod/go.sum, а не при любом изменении кода — COPY . . ниже
# делается уже после go mod download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

# ---- runtime stage ----
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /out/server .
COPY --from=builder /out/worker .

# web/ (HTML/CSS/JS) в образ отдельно не копируем — эти файлы вшиты прямо
# в бинарник ещё на этапе сборки через go:embed (см. web/embed.go,
# инкремент 7). А вот миграции остаются обычными файлами на диске
# (golang-migrate читает их через file://), поэтому их копируем.
COPY --from=builder /app/migrations ./migrations
ENV MIGRATIONS_PATH=/app/migrations

EXPOSE 8080

# По умолчанию контейнер запускает HTTP-сервер; для воркера
# docker-compose.yml переопределяет команду на ["./worker"].
CMD ["./server"]
