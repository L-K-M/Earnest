SHELL    := /bin/bash
GO       ?= go
IMAGE    ?= earnest
VERSION  ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: build test lint docker-build compose-check check

build:
	$(GO) build -trimpath \
	  -ldflags "-s -w -X main.version=$(VERSION) -X main.revision=$(REVISION)" \
	  -o dist/earnest ./cmd/earnest

test:
	$(GO) test -race -count=1 ./...

lint:
	test -z "$$(gofmt -l cmd)"
	$(GO) vet ./...

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) \
	  -t $(IMAGE):local .

compose-check:
	docker compose -f compose.yaml config --quiet

# Mirrors CI exactly: the Go checks, the compose validation and the image build.
check: lint test compose-check docker-build
