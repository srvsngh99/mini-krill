# NOTE: The public build (`make install`) intentionally EXCLUDES the Reef feed
# and the Chroma-backed social memory. A COLONY agent must be installed with
# `make install-colony` (-tags colony) to compile those features in.
VERSION := $(shell cat VERSION)
BINARY  := minikrill
LDFLAGS := -s -w -X github.com/srvsngh99/mini-krill/internal/core.Version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build test lint clean install install-colony docker docker-down release fmt vet check help

## build: Compile the binary to ./dist/minikrill
build:
	@mkdir -p dist
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY) ./cmd/minikrill
	@echo "Built dist/$(BINARY) $(VERSION)"

## test: Run all tests with race detector
test:
	go test ./... -v -race

## lint: Run golangci-lint
lint:
	golangci-lint run

## clean: Remove build artifacts
clean:
	rm -rf dist/

## install: Install the public binary (no feed / social memory) via go install
install:
	CGO_ENABLED=0 go install -ldflags="$(LDFLAGS)" ./cmd/minikrill
	@echo "Installed $(BINARY) $(VERSION) (public build)"

## install-colony: Install the COLONY binary WITH feed + social memory (-tags colony)
install-colony:
	CGO_ENABLED=0 go install -tags colony -ldflags="$(LDFLAGS)" ./cmd/minikrill
	@echo "Installed $(BINARY) $(VERSION) (colony build: feed + social memory)"

## docker: Build and start all containers
docker:
	docker-compose up --build -d

## docker-down: Stop and remove all containers
docker-down:
	docker-compose down

## release: Create a snapshot release with goreleaser
release:
	goreleaser release --snapshot --clean

## fmt: Format all Go source files
fmt:
	gofmt -s -w .

## vet: Run go vet on all packages
vet:
	go vet ./...

## check: Run all quality checks (fmt, vet, lint, test)
check: fmt vet lint test

## help: Print this help message
help:
	@echo "Mini Krill v$(VERSION) - available targets:"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
