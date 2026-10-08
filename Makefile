.PHONY: help build run test test-scripts leak-guard test-staticsite-js clean docker-up docker-up-fg docker-down migrate-up migrate-down migrate-down-all lint

help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building..."
	@go build -ldflags "-X main.Commit=$$(git rev-parse --short HEAD)" -o bin/server cmd/server/main.go

run: ## Run the application
	@echo "Running..."
	@go run cmd/server/main.go

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

test-short: ## Run tests (short output)
	@echo "Running tests..."
	@go test ./...

test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-race: ## Run tests with race detector
	@echo "Running tests with race detector..."
	@go test -race ./...

test-scripts: ## Run shell tests for scripts/ (no Docker needed)
	@echo "Running shell tests..."
	@for t in scripts/tests/*_test.sh; do echo "== $$t"; bash "$$t" || { [ -n "$$GITHUB_ACTIONS" ] && echo "::error title=test-scripts::упал $$t"; exit 1; }; done

leak-guard: ## Check tracked files for secrets and instance words (LEAK_DENYLIST=file adds words)
	@scripts/leak-guard.sh

test-staticsite-js: ## Run JS tests of the static reading room (node, no browser)
	@node --test internal/staticsite/assets/search.test.cjs

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html

docker-up: ## Start Docker services
	@echo "Starting Docker services..."
	@docker compose up -d

docker-up-fg: ## Start Docker services in foreground
	@echo "Starting Docker services..."
	@docker compose up

docker-down: ## Stop Docker services
	@echo "Stopping Docker services..."
	@docker compose down

docker-logs: ## View Docker logs
	@docker compose logs -f

migrate-up: ## Run database migrations up
	@echo "Running migrations up..."
	@go run cmd/migrate/main.go up

migrate-down: ## Roll back ONE migration
	@echo "Rolling back one migration..."
	@go run cmd/migrate/main.go down

migrate-down-all: ## DANGER: откатывает ВСЕ миграции и удаляет все данные
	@echo "ВНИМАНИЕ: это откатит ВСЕ миграции и удалит все таблицы и данные."
	@read -p "Введите имя базы для подтверждения: " name; \
	MIGRATE_CONFIRM="$$name" go run cmd/migrate/main.go down-all

migrate-create: ## Create new migration (use NAME=migration_name)
	@echo "Creating migration..."
	@migrate create -ext sql -dir internal/database/migrations -seq $(NAME)

lint: ## Run linter
	@echo "Running linter..."
	@golangci-lint run

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...

tidy: ## Tidy dependencies
	@echo "Tidying dependencies..."
	@go mod tidy

install-tools: ## Install development tools
	@echo "Installing tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

.DEFAULT_GOAL := help

