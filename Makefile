# Relay build automation.

BINARY      := relay
PKG         := github.com/AymanYouss/relay
CMD         := ./cmd/relay
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X $(PKG)/internal/app.Version=$(VERSION)
GOFILES     := $(shell find . -name '*.go' -not -path './web/*')
WEBDIR      := web
EMBEDDIR    := internal/server/webui/dist

.PHONY: all
all: web build

.PHONY: build
build: ## Build the relay binary (expects embedded web assets to exist)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

.PHONY: run
run: ## Run the gateway with the local config
	go run $(CMD) -config relay.yaml

.PHONY: test
test: ## Run the full test suite with the race detector
	go test -race -covermode=atomic -coverprofile=coverage.txt ./...

.PHONY: cover
cover: test ## Show coverage summary
	go tool cover -func=coverage.txt | tail -1

.PHONY: bench
bench: ## Run benchmarks
	go test -run=^$$ -bench=. -benchmem ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint (install: https://golangci-lint.run)
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Tidy go modules
	go mod tidy

.PHONY: web
web: ## Build the dashboard and embed it into the binary
	cd $(WEBDIR) && pnpm install --frozen-lockfile && pnpm build
	rm -rf $(EMBEDDIR)
	mkdir -p $(EMBEDDIR)
	cp -r $(WEBDIR)/dist/* $(EMBEDDIR)/

.PHONY: web-dev
web-dev: ## Run the dashboard dev server
	cd $(WEBDIR) && pnpm install && pnpm dev

.PHONY: docker
docker: ## Build the container image
	docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

.PHONY: compose-up
compose-up: ## Start the full local stack (gateway, redis, prometheus, grafana)
	docker compose up --build

.PHONY: compose-down
compose-down:
	docker compose down -v

.PHONY: clean
clean:
	rm -rf bin coverage.txt

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'
