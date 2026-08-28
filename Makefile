.PHONY: run run-worker build fmt vet tidy lint test test-integration cover

run:
	go run ./cmd/server

run-worker:
	go run ./cmd/worker

build:
	go build -o bin/server ./cmd/server
	go build -o bin/worker ./cmd/worker

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

lint:
	golangci-lint run ./...

# Быстрые юнит-тесты: без Docker, без сети, гоняются на каждый коммит.
# cmd/... исключены из покрытия — это только main() и сборка зависимостей
# (см. README), там нечего юнит-тестировать, только вручную/интеграционно.
test:
	go test $$(go list ./... | grep -v '/cmd/') -race -coverprofile=coverage.out

# Медленные интеграционные тесты: поднимают реальный Postgres в Docker
# через testcontainers-go. Требуют запущенный Docker.
test-integration:
	go test -tags=integration ./tests/... -v

# Показать процент покрытия по пакетам после `make test`.
cover:
	go tool cover -func=coverage.out
