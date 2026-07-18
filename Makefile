VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: test bench lint fmt generate clean mod-tidy coverage help

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

test: ## Run all tests with race detection and coverage profile
	mkdir -p coverage
	go test -race -covermode=atomic -coverprofile=coverage/coverage.out ./...
	grep -Ev '(\.gen\.go|/[^/]*gen/[^/:]+\.go):' coverage/coverage.out > coverage/coverage.filtered.out
	mv coverage/coverage.filtered.out coverage/coverage.out

bench: ## Run benchmarks
	go test -bench=. -benchmem -run=^$$ ./...

generate: ## Run code generators (no-op when no //go:generate directives)
	go generate ./...

coverage: ## Generate HTML coverage report from coverage/coverage.out
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html

lint: ## Run linter
	golangci-lint run --timeout=5m

fmt: ## Format code
	golangci-lint fmt

clean: ## Clean build artifacts
	rm -rf coverage

mod-tidy: ## Tidy Go modules
	go mod tidy
