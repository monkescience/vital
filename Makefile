SHELL := /bin/bash

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt
