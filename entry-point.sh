#!/bin/bash
set -e

# Run migrations
echo "Running database migrations..."
migrate -path ./migrations -database "$DSN" goto 1
# migrate -path ./migrations -database "$DSN" goto 2

# Setting max_wal_size
psql $DSN -c "ALTER SYSTEM SET max_wal_size = '2GB';"
psql $DSN -c "SELECT pg_reload_conf();"

# Start the main application
echo "Starting application..."
exec "$@"
