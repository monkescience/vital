VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: test lint fmt build clean mod-tidy coverage coverage-html coverage-total help

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

test: ## Run tests
	go test -race ./...

build: ## Build all packages
	go build ./...

coverage-total: ## Print total coverage percent
	@go tool cover -func=coverage.out | awk '/^total:/ {gsub("%", "", $$3); print $$3}'

coverage: ## Run tests with coverage
	go test -race -coverprofile=coverage.out -covermode=atomic ./...

coverage-html: ## Generate HTML coverage report
	$(MAKE) coverage
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	golangci-lint run --timeout=5m

fmt: ## Format code
	golangci-lint fmt

clean: ## Clean build artifacts
	rm -f coverage.out coverage.html

mod-tidy: ## Tidy Go modules
	go mod tidy
