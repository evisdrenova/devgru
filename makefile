.PHONY: build clean test install dev help

# Default target
help: ## Show this help message
	@echo "DevGru Monorepo Commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Build commands
build: build-go build-extension ## Build everything
	@echo "✅ Build complete!"

build-go: ## Build Go CLI binary
	@echo "🔨 Building Go binary..."
	@go build -o bin/devgru ./cmd/devgru
	@echo "✅ Go binary built: bin/devgru"

build-extension: ## Build VS Code extension
	@echo "🔨 Building VS Code extension..."
	@cd vscode-extension && npm run compile
	@echo "✅ Extension built"

package-extension: ## Package VS Code extension as VSIX
	@echo "📦 Packaging VS Code extension..."
	@./scripts/build-extension.sh

# Development commands
dev: ## Start development mode (watches for changes)
	@echo "🚀 Starting development mode..."
	@make build-go
	@cd vscode-extension && npm run watch &
	@echo "👀 Watching for changes..."

install: ## Install all dependencies
	@echo "📦 Installing dependencies..."
	@go mod download
	@npm install
	@echo "✅ Dependencies installed"

# Testing commands
test: test-go test-extension ## Run all tests

test-go: ## Run Go tests
	@echo "🧪 Running Go tests..."
	@go test ./...

test-extension: ## Run extension tests
	@echo "🧪 Running extension tests..."
	@cd vscode-extension && npm test

# Quality commands
lint: ## Run linters
	@echo "🔍 Linting Go code..."
	@go vet ./...
	@echo "🔍 Linting TypeScript code..."
	@cd vscode-extension && npm run lint

format: ## Format code
	@echo "✨ Formatting Go code..."
	@go fmt ./...
	@echo "✨ Formatting TypeScript code..."
	@cd vscode-extension && npm run format

# Utility commands
clean: ## Clean build artifacts
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf vscode-extension/out/
	@rm -rf vscode-extension/*.vsix
	@echo "✅ Clean complete"

run: build-go ## Build and run DevGru CLI
	@echo "🚀 Running DevGru..."
	@./bin/devgru

run-ide: build-go ## Build and run DevGru IDE server
	@echo "🚀 Starting DevGru IDE server..."
	@./bin/devgru ide connect

# Release commands
release: build package-extension ## Build everything for release
	@echo "🚀 Release build complete!"
	@echo "📁 CLI binary: bin/devgru"
	@echo "📦 Extension: vscode-extension/*.vsix"

# Docker commands (optional)
docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	@docker build -t devgru:latest .

# Installation helpers
install-go-deps: ## Install Go dependencies
	@go mod download

install-node-deps: ## Install Node.js dependencies
	@npm install

install-extension: package-extension ## Install VS Code extension locally
	@echo "📦 Installing VS Code extension..."
	@cd vscode-extension && code --install-extension devgru-code-*.vsix
	@echo "✅ Extension installed"