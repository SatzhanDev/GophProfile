.PHONY: run run-worker run-migrate build fmt vet tidy lint test test-integration cover helm-lint helm-template

run:
	go run ./cmd/server

run-worker:
	go run ./cmd/worker

# cmd/migrate — тот же раннер миграций, что встроен в server/worker при
# старте, но отдельным короткоживущим бинарником: используется Helm
# pre-upgrade hook'ом (k8s/templates/job-migrate.yaml), но пригодится и
# локально — накатить миграции без поднятия всего сервера целиком.
run-migrate:
	go run ./cmd/migrate

build:
	go build -o bin/server ./cmd/server
	go build -o bin/worker ./cmd/worker
	go build -o bin/migrate ./cmd/migrate

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

# Статическая проверка Helm-чарта: синтаксис values.yaml/Chart.yaml,
# обязательные поля, отсутствие явных ошибок в шаблонах. Не проверяет
# соответствие реальной схеме объектов Kubernetes — для этого уже нужен
# helm template + kubectl apply --dry-run=server на настоящем кластере.
helm-lint:
	helm lint ./k8s
	helm lint ./k8s -f k8s/values-dev.yaml
	helm lint ./k8s -f k8s/values-prod.yaml

# Отрендерить итоговые манифесты без применения к кластеру — удобно
# посмотреть глазами, что именно получится из шаблонов + values.
helm-template:
	helm template gophprofile ./k8s -f k8s/values-dev.yaml
