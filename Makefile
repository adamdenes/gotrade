# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build -race
GOTEST=$(GOCMD) test

BINARY_NAME=gotrade

build:
	@echo "Building binary..."
	$(GOBUILD) -o ./bin/$(BINARY_NAME) -v cmd/gotrade/main.go

test:
	$(GOTEST) ./...

run: build
	./bin/$(BINARY_NAME)


.PHONY: build test run