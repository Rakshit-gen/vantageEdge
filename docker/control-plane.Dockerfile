# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application and migrator
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o control-plane ./cmd/control-plane && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o migrator ./cmd/migrator

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

# Copy binaries from builder
COPY --from=builder /app/control-plane .
COPY --from=builder /app/migrator .

# Copy migrations (if needed for embedded migrations)
COPY --from=builder /app/migrations ./migrations

# Expose ports
EXPOSE 8080 9090 9091

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

# Run the application
CMD ["./control-plane"]
