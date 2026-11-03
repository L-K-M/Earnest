SHELL    := /bin/bash
GO       ?= go
IMAGE    ?= earnest
VERSION  ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: build test lint docker-build compose-check check web-install web-build web-check

web-install:
	cd web && pnpm install --frozen-lockfile

web-build: web-install
	cd web && pnpm build
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	cp -R web/dist/. internal/web/dist/

web-check: web-install
	cd web && pnpm typecheck && pnpm lint && pnpm test && pnpm build && pnpm check-generated

build: web-build
	$(GO) build -trimpath \
	  -ldflags "-s -w -X main.version=$(VERSION) -X main.revision=$(REVISION)" \
	  -o dist/earnest ./cmd/earnest

test:
	$(GO) test -race -count=1 ./...
	sh scripts/backup_test.sh

lint:
	test -z "$$(gofmt -l cmd internal)"
	$(GO) vet ./...

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) \
	  -t $(IMAGE):local .

compose-check:
	docker compose -f compose.yaml config --quiet

# Mirrors CI exactly: the Go checks, the compose validation and the image build.
check: lint test web-check compose-check docker-build
