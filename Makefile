.PHONY: help build run test clean docker-up docker-down migrate-up migrate-down seed

# Variables
APP_NAME=vantageedge
CONTROL_PLANE_BINARY=control-plane
GATEWAY_BINARY=gateway
MIGRATOR_BINARY=migrator

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Build directories
BUILD_DIR=build
CMD_DIR=cmd

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

install: ## Install dependencies
	$(GOMOD) download
	$(GOMOD) tidy

build-control-plane: ## Build control plane binary
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(CONTROL_PLANE_BINARY) $(CMD_DIR)/control-plane/main.go

build-gateway: ## Build gateway binary
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(GATEWAY_BINARY) $(CMD_DIR)/gateway/main.go

build-migrator: ## Build migrator binary
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(MIGRATOR_BINARY) $(CMD_DIR)/migrator/main.go

build: build-control-plane build-gateway build-migrator ## Build all binaries

run-control-plane: build-control-plane ## Run control plane service
	./$(BUILD_DIR)/$(CONTROL_PLANE_BINARY)

run-gateway: build-gateway ## Run gateway service
	./$(BUILD_DIR)/$(GATEWAY_BINARY)

test: ## Run tests
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests with coverage report
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-integration: ## Run integration tests
	$(GOTEST) -v -tags=integration ./...

fmt: ## Format code
	$(GOFMT) ./...

lint: ## Run linter
	golangci-lint run ./...

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

docker-build: ## Build Docker images
	docker-compose build

docker-up: ## Start all services with Docker Compose
	docker-compose up -d

docker-down: ## Stop all services
	docker-compose down

docker-logs: ## View logs
	docker-compose logs -f

docker-restart: docker-down docker-up ## Restart all services

migrate-up: ## Run database migrations up
	@echo "Running migrations..."
	$(GOCMD) run $(CMD_DIR)/migrator/main.go up

migrate-down: ## Run database migrations down
	@echo "Rolling back migrations..."
	$(GOCMD) run $(CMD_DIR)/migrator/main.go down

migrate-create: ## Create a new migration (usage: make migrate-create name=create_users_table)
	@if [ -z "$(name)" ]; then \
		echo "Error: name is required. Usage: make migrate-create name=create_users_table"; \
		exit 1; \
	fi
	@timestamp=$$(date +%Y%m%d%H%M%S); \
	touch migrations/$${timestamp}_$(name).up.sql; \
	touch migrations/$${timestamp}_$(name).down.sql; \
	echo "Created migrations/$${timestamp}_$(name).up.sql"; \
	echo "Created migrations/$${timestamp}_$(name).down.sql"

seed: ## Seed database with sample data
	@echo "Seeding database..."
	psql "postgresql://vantageedge:changeme_db_password@localhost:5432/vantageedge?sslmode=disable" -f scripts/seed.sql

dev: ## Run development environment
	docker-compose up postgres redis -d
	@echo "Waiting for services to be ready..."
	@sleep 5
	$(MAKE) migrate-up
	$(MAKE) seed

proto-gen: ## Generate gRPC code from proto files
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/*.proto

openapi-gen: ## Generate OpenAPI documentation
	swag init -g cmd/control-plane/main.go -o api/openapi

load-test: ## Run load tests (requires hey tool)
	@if ! command -v hey &> /dev/null; then \
		echo "Installing hey..."; \
		go install github.com/rakyll/hey@latest; \
	fi
	hey -n 10000 -c 100 -m GET http://localhost:8000/health

benchmark: ## Run Go benchmarks
	$(GOTEST) -bench=. -benchmem ./...

security-scan: ## Run security scan (requires gosec)
	@if ! command -v gosec &> /dev/null; then \
		echo "Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...

deps-update: ## Update dependencies
	$(GOGET) -u ./...
	$(GOMOD) tidy

deps-check: ## Check for dependency updates
	$(GOCMD) list -u -m all

docker-clean: ## Remove all Docker containers, images, and volumes
	docker-compose down -v
	docker system prune -af

all: install build test ## Install, build, and test

.DEFAULT_GOAL := help
