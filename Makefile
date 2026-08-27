.PHONY: run run-worker build fmt vet tidy lint

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
