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
# migrate — короткоживущий бинарник только для применения миграций,
# используется Helm pre-upgrade hook'ом (k8s/templates/job-migrate.yaml),
# сам по себе HTTP не поднимает и ничего не слушает.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

# ---- runtime stage ----
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata

# Непривилегированный пользователь вместо root — принцип наименьших
# привилегий применяем уже на уровне образа, а не только через
# Kubernetes SecurityContext (k8s/values.yaml): так образ безопасен по
# умолчанию, даже если его запустят просто `docker run` или через
# docker-compose, без единой настройки securityContext.
# UID/GID 65532 — общепринятое значение для "nonroot" (то же число
# использует, например, дистроless-образы Google) — специально берём то
# же самое число в k8s/values.yaml (podSecurityContext.runAsUser/
# runAsGroup/fsGroup), чтобы в двух местах не оказалось два разных
# "непривилегированных" пользователя.
RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S -G nonroot nonroot

WORKDIR /app

COPY --from=builder /out/server .
COPY --from=builder /out/worker .
COPY --from=builder /out/migrate .

# web/ (HTML/CSS/JS) в образ отдельно не копируем — эти файлы вшиты прямо
# в бинарник ещё на этапе сборки через go:embed (см. web/embed.go,
# инкремент 7). А вот миграции остаются обычными файлами на диске
# (golang-migrate читает их через file://), поэтому их копируем.
COPY --from=builder /app/migrations ./migrations
ENV MIGRATIONS_PATH=/app/migrations

# chown обязательно ПОСЛЕ всех COPY: сами COPY --from=builder создают
# файлы от имени root (сборка Docker всегда идёт от root), поменять
# владельца можно только когда файлы уже лежат на месте.
RUN chown -R nonroot:nonroot /app
USER nonroot:nonroot

EXPOSE 8080
# 9100 — /metrics отдельного HTTP-сервера воркера (см. cmd/worker/main.go).
# server этот порт не слушает, но EXPOSE ничего не стоит и документирует
# оба возможных случая использования образа сразу в одном месте.
EXPOSE 9100

# По умолчанию контейнер запускает HTTP-сервер; для воркера
# docker-compose.yml переопределяет команду на ["./worker"].
CMD ["./server"]
