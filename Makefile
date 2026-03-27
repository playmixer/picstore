GIT_HASH := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_VERSION ?= $(if $(GIT_HASH),$(GIT_HASH),$(shell date +%Y%m%d%H%M%S))

reg:
	docker build --build-arg BUILD_VERSION=$(BUILD_VERSION) -t registry.mix.local/mixer/picstore .
	docker push registry.mix.local/mixer/picstore

up-store:
	docker compose up pic-database pic-redis

run:
	BUILD_VERSION=$(BUILD_VERSION) go run ./cmd/main/main.go

down:
	docker compose down

build:
	go build -ldflags "-X main.buildVersion=$(BUILD_VERSION)" -o bin/picstore.exe ./cmd/main/main.go