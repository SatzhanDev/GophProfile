.PHONY: run build fmt vet tidy lint

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

lint:
	golangci-lint run ./...
