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

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o migrator ./cmd/migrator

# Final stage
FROM alpine:latest

# Install postgresql-client and wait-for-it
RUN apk --no-cache add postgresql-client bash

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/migrator .
COPY --from=builder /app/migrations ./migrations

# Add wait-for-it script
COPY docker/wait-for-it.sh ./
RUN chmod +x ./wait-for-it.sh

# Run migrations
CMD ["./migrator", "up"]
