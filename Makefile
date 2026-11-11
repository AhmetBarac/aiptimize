SHELL := /bin/sh

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: all fmt check-fmt vet test test-race web-install web-test web-build build run docker docker-run clean

all: check-fmt vet test-race web-test web-build build

fmt:
	gofmt -l -w cmd internal

check-fmt:
	@files="$$(gofmt -l cmd internal)"; \
	if [ -n "$$files" ]; then echo "gofmt required for:"; echo "$$files"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

web-install:
	cd web && npm ci

web-test:
	cd web && npm test

web-build:
	cd web && npm run build

build: web-build
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/aiptimize ./cmd/aiptimize

run:
	go run ./cmd/aiptimize serve

docker:
	docker buildx build --platform linux/arm64 -f deploy/Dockerfile -t aiptimize:local .

docker-run:
	docker run --rm --name aiptimize -p 8080:8080 --env-file .env \
		-v aiptimize-v1-data:/data docker.io/ahmetbarac/aiptimize:dev

e2e:
	cd web && npm run test:e2e

clean:
	rm -rf bin web/dist internal/webui/dist/assets internal/webui/dist/index.html
