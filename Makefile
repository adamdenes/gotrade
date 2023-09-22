# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build -race
GOTEST=$(GOCMD) test
BINARY_NAME=gotrade

# Postgres
DB_CONTAINER_NAME=postgres
DB_NAME=binance_db
DB_PORT=5432
DB_USER=root

build:
	@echo "Building binary..."
	$(GOBUILD) -o ./bin/$(BINARY_NAME) -v cmd/gotrade/main.go

test:
	$(GOTEST) ./...

run: build
	./bin/$(BINARY_NAME)

help:
	@echo "Available targets:"
	@echo "  start_db           - Start the PostgreSQL Docker container"
	@echo "  stop_db            - Stop the PostgreSQL Docker container"
	@echo "  connect_db         - Connect to the PostgreSQL database using psql"


start_db:
	docker run --name $(DB_CONTAINER_NAME) -e POSTGRES_PASSWORD=$(DB_PASSWORD) -e POSTGRES_USER=$(DB_USER) -e POSTGRES_DB=$(DB_NAME) -p $(DB_PORT):$(DB_PORT) -d postgres

stop_db:
	docker stop $(DB_CONTAINER_NAME)
	docker rm $(DB_CONTAINER_NAME)

connect_db:
	docker exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -p $(DB_PORT)

.PHONY: build test run start_db stop_db connect_db