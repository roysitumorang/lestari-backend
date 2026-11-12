PROJECTNAME=lestari
GOBASE=$(shell pwd)
MODULE=github.com/roysitumorang/lestari-backend
# the port of APP_ADDR in .env (":8080", "127.0.0.1:8080" or "8080"); 8080 when it is not set
PORT_HTTP=$(shell p=`grep -s '^APP_ADDR=' .env | tail -n 1 | sed -e 's/^APP_ADDR=//' -e 's/["'"'"' ]//g' -e 's/^.*://'`; echo $${p:-8080})
LDFLAGS=-X $(MODULE)/config.AppName=$(PROJECTNAME) \
	-X $(MODULE)/config.Commit=$(shell git rev-list -1 HEAD) \
	-X $(MODULE)/config.Build=$(shell date +%FT%T%:z)
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/lestari_test?sslmode=disable

.PHONY: all format build release run stop migrate seed routes doc test test-integration lint upgrade-dependencies profile docker

format:
	@find . -name "*.go" -not -path "./.git/*" | xargs gofmt -s -d -w

build:
	@go mod tidy
	@CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(PROJECTNAME)

release:
	@go mod tidy
	@CGO_ENABLED=0 go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o $(PROJECTNAME)

run: build stop
	@-nohup $(GOBASE)/$(PROJECTNAME) run > /dev/null 2>&1 & echo " > $(PROJECTNAME) is available at port $(PORT_HTTP) and PID $$!"

stop:
	@-lsof -t -i :$(PORT_HTTP) | xargs --no-run-if-empty kill

migrate: build
	@$(GOBASE)/$(PROJECTNAME) migration run

seed: build
	@$(GOBASE)/$(PROJECTNAME) seed

routes: build
	@$(GOBASE)/$(PROJECTNAME) routes

doc:
	@echo Starting swagger generating
	@swag fmt -d ./ --exclude ./docs && swag init -g main.go --parseInternal

test:
	@go test ./...

test-integration:
	@ENV=production TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test -count=1 ./...

lint:
	@staticcheck ./... && golangci-lint run ./...

upgrade-dependencies:
	@echo Upgrading dependencies
	@go get -u ./... && go mod tidy

profile:
	@curl "http://localhost:$(PORT_HTTP)/debug/pprof/profile?seconds=30" > default.pgo

docker:
	@docker compose up --build
