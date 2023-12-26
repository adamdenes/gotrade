#!/bin/bash
set -e

# Run migrations
echo "Running database migrations..."
migrate -path ./migrations -database "$DSN" goto 1
migrate -path ./migrations -database "$DSN" goto 2

# Start the main application
echo "Starting application..."
exec "$@"
