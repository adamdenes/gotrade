#!/bin/bash
set -e

# Run migrations
echo "Running database migrations..."
migrate -path ./migrations -database "$DSN" goto 1

# Start the main application
echo "Starting application..."
exec "$@"
