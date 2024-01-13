# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build -race
GOTEST=$(GOCMD) test
BINARY_NAME=gotrade

# Postgres
DB_CONTAINER_NAME=gotrade-timescaledb-1
DB_NAME=binance_db
DB_PORT=5432
DB_USER=web
DB_VOLUME=timescaledb_data
DB_VOLUME_PATH=/var/lib/postgresql/data

VERSION=""

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
	@echo "  build              	- Build project"
	@echo "  run                	- Build and run project"
	@echo "  test               	- Test all go test files"
	@echo "  help               	- Print this message"
	@echo "  start_db           	- Start the PostgreSQL Docker container"
	@echo "  stop_db            	- Stop the PostgreSQL Docker container"
	@echo "  connect_db         	- Connect to the PostgreSQL database using psql"
	@echo "  migrate-up         	- Run the hypertable migration"
	@echo "  migrate-cagg       	- Run the aggregate migration (USE ONLY AFTER DATA IS PRE-LOADED)"
	@echo "  migrate-down       	- Revert the hypertable migration"
	@echo "  migrate-fix        	- Fix migration errors"
	@echo "  clean-dangling-images  - Remove dangling docker images"

clean-dangling-images:
	docker image prune -f

start_db: clean-dangling-images
	docker volume create $(DB_VOLUME)
	docker run --name $(DB_CONTAINER_NAME) -v $(DB_VOLUME):$(DB_VOLUME_PATH)  -e POSTGRES_PASSWORD=$(DB_PASSWORD) -e POSTGRES_USER=$(DB_USER) -e POSTGRES_DB=$(DB_NAME) -p $(DB_PORT):$(DB_PORT) -d timescale/timescaledb-ha:pg15-latest

stop_db:
	docker stop $(DB_CONTAINER_NAME)
	docker rm $(DB_CONTAINER_NAME)

connect_db:
	docker exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -p $(DB_PORT)

migrate-up:
	@echo "Running hypertable migration"
	migrate -path ./migrations -database $(DSN) goto 1

migrate-cagg:
	@echo "Running continuous aggregates migration/refresh"
	migrate -path ./migrations -database $(DSN) goto 2 
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_1w', now()::timestamp - INTERVAL '1 year', now()::timestamp);"
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_1d', now()::timestamp - INTERVAL '1 year', now()::timestamp);"
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_4h', now()::timestamp - INTERVAL '1 year', now()::timestamp);"
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_1h', now()::timestamp - INTERVAL '1 year', now()::timestamp);"
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_5m', now()::timestamp - INTERVAL '1 year', now()::timestamp);"
	psql $(DSN) -c "CALL refresh_continuous_aggregate('binance.aggregate_1m', now()::timestamp - INTERVAL '1 year', now()::timestamp);"

migrate-down:
	@echo "Reverting hypertable migration"
	migrate -path ./migrations -database $(DSN) down

migrate-fix:
	@echo "Fixing hypertable migration"
	migrate -path ./migrations -database $(DSN) force $(VERSION)


.PHONY: build test run stop start_db stop_db connect_db migrate-up migrate-cagg migrate-down migrate-fix clean-dangling-images
