# Loads .env when present.
ifneq (,$(wildcard .env))
include .env
export
endif

TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/lestari_test?sslmode=disable

.PHONY: run build migrate seed routes test test-integration lint docker

run:
	go run ./cmd/lestari serve

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/lestari ./cmd/lestari

migrate:
	go run ./cmd/lestari migrate

seed:
	go run ./cmd/lestari seed

routes:
	go run ./cmd/lestari routes

test:
	go test ./...

test-integration:
	APP_ENV=production TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test -count=1 ./...

# golangci-lint v2.15.0 and staticcheck 2026.2.2 (see .golangci.yml)
lint:
	go vet ./...
	staticcheck -checks all ./...
	golangci-lint run ./...

docker:
	docker build -t lestari-backend .
