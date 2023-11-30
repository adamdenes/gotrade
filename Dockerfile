FROM golang:1.21 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o gotrade -v cmd/gotrade/main.go

# Start the final stage
FROM alpine:latest  

# Install dependencies
RUN apk --no-cache add ca-certificates bash wget

# Set the working directory
WORKDIR /root/

# Download and install go-migrate
RUN wget https://github.com/golang-migrate/migrate/releases/download/v4.15.1/migrate.linux-amd64.tar.gz -O migrate.tar.gz && \
    tar -xzvf migrate.tar.gz && \
    mv migrate /usr/local/bin/migrate && \
    chmod +x /usr/local/bin/migrate && \
    rm migrate.tar.gz

# Copy the built binary from the builder stage
COPY --from=builder /app/gotrade /root/gotrade
# Copy the entrypoint script to the final image
COPY --from=builder /app/entry-point.sh /root/entry-point.sh
# Copy migration files
COPY --from=builder /app/migrations /root/migrations
# Copy template files for webserver
COPY --from=builder /app/web /root/web

# Make the script executable
RUN chmod +x /root/entry-point.sh

# Set the entrypoint
ENTRYPOINT ["./entry-point.sh"]

# Default command
CMD ["./gotrade"]

# Expose the port the app runs on
EXPOSE 4000
