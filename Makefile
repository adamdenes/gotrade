# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build -race
GOTEST=$(GOCMD) test
BINARY_NAME=gotrade

# Postgres
DB_CONTAINER_NAME=timescale
DB_NAME=binance_db
DB_PORT=5432
DB_USER=web
DB_VOLUME=my-postgres-data
DB_VOLUME_PATH=/home/adenes/docker_volumes/data

build:
	@echo "Building binary..."
	$(GOBUILD) -o ./bin/$(BINARY_NAME) -v cmd/gotrade/main.go

test:
	$(GOTEST) -v ./...

run: build
	./bin/$(BINARY_NAME)

stop: 
	@echo "Stopping server..."
	@-pkill -SIGTERM -f "./bin/$(BINARY_NAME)"
	@echo "Server stopped!"

help:
	@echo "Available targets:"
	@echo "  build              - Build project"
	@echo "  run                - Build and run project"
	@echo "  test               - Test all go test files"
	@echo "  help               - Print this message"
	@echo "  start_db           - Start the PostgreSQL Docker container"
	@echo "  stop_db            - Stop the PostgreSQL Docker container"
	@echo "  connect_db         - Connect to the PostgreSQL database using psql"


start_db:
	docker volume create $(DB_VOLUME)
	docker run --name $(DB_CONTAINER_NAME) -v $(DB_VOLUME):$(DB_VOLUME_PATH)  -e POSTGRES_PASSWORD=$(DB_PASSWORD) -e POSTGRES_USER=$(DB_USER) -e POSTGRES_DB=$(DB_NAME) -p $(DB_PORT):$(DB_PORT) -d timescale/timescaledb-ha:pg14-latest

stop_db:
	docker stop $(DB_CONTAINER_NAME)
	docker rm $(DB_CONTAINER_NAME)
	docker volume rm $(DB_VOLUME)

connect_db:
	docker exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -p $(DB_PORT)

.PHONY: build test run stop start_db stop_db connect_db
