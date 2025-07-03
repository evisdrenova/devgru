EXT_VERSION := $(shell node -p "require('./vscode-extension/package.json').version")

# Makefile for DevGru
.PHONY: build clean install test run dev extension-build extension-install reload-vscode dev-build help

# Development build - builds everything and reloads VS Code
dev-build: go-build extension-build extension-install reload-vscode
	@echo "🚀 Development build complete! VS Code should reload automatically."

# Default target - production build
build: go-build extension-build

# Build Go binary
go-build:
	@echo "🔨 Building DevGru binary..."
	@mkdir -p bin
	go build -o bin/devgru ./cmd/devgru
	@echo "✅ Go binary built successfully"


# Build VS Code extension
extension-build:
	@echo "🔨 Building DevGru VS Code Extension v$(EXT_VERSION)…"
	@if [ -d "vscode-extension" ]; then \
	  cd vscode-extension && \
	  npm install --silent && \
	  npx vsce package --out devgru-code-$(EXT_VERSION).vsix && \
	  echo "✅ VS Code extension built successfully: devgru-code-$(EXT_VERSION).vsix"; \
	else \
	  echo "❌ vscode-extension directory not found"; \
	 exit 1; \
	fi
# Install/update VS Code extension
extension-install: extension-build
	@echo "📦 Installing VS Code extension..."
	@cd vscode-extension && \
	if ls devgru-code-*.vsix >/dev/null 2>&1; then \
	  VSIX_FILE=$$(ls devgru-code-*.vsix | head -1); \
	  echo "Installing: $$VSIX_FILE"; \
	  code --install-extension "$$VSIX_FILE" --force; \
	  echo "✅ VS Code extension installed successfully"; \
	else \
	  echo "❌ No .vsix package found in vscode-extension/"; \
	  ls -1 | grep --color=never "\.vsix$$" || echo "(none)"; \
	  exit 1; \
	fi

# Reload VS Code window
reload-vscode:
	@echo "🔄 Reloading VS Code window..."
	@osascript -e 'tell application "Visual Studio Code" to activate' > /dev/null 2>&1 || true
	@sleep 0.5
	@osascript -e 'tell application "System Events" to tell process "Visual Studio Code" to keystroke "r" using {command down, shift down}' > /dev/null 2>&1 || true
	@echo "✅ VS Code reload triggered"

# Fast development cycle - watch for changes and rebuild
dev-watch:
	@echo "👀 Watching for changes... (Press Ctrl+C to stop)"
	@while true; do \
		fswatch -1 . --exclude=".git" --exclude="node_modules" --exclude="bin" --exclude="*.vsix" && \
		echo "🔄 Changes detected, rebuilding..." && \
		make dev-build && \
		echo "⏱️  Waiting for next change..."; \
	done

# Build the UI components (if you have any UI build steps)
ui-build:
	@echo "🔨 Building UI components..."
	# Add any UI build steps here if needed
	# For now, this ensures the ui package is included
	go build -o /dev/null ./ui
	@echo "✅ UI components built successfully"

# Install binary to system PATH
install: build
	@echo "📦 Installing DevGru to system PATH..."
	sudo cp bin/devgru /usr/local/bin/
	@echo "✅ DevGru installed to /usr/local/bin/"

# Run tests
test:
	@echo "🧪 Running tests..."
	go test ./...

# Run in development mode (with race detector)
dev:
	@echo "🏃 Running DevGru in development mode..."
	go run -race ./cmd/devgru

# Run the built binary
run: build
	@echo "🏃 Running DevGru..."
	./bin/devgru

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf bin/
	@if [ -d "vscode-extension" ]; then \
		cd vscode-extension && rm -f *.vsix && rm -rf node_modules; \
	fi
	go clean
	@echo "✅ Clean complete"

# Setup development environment
setup:
	@echo "⚙️ Setting up development environment..."
	@if [ -d "vscode-extension" ]; then \
		cd vscode-extension && npm install; \
	fi
	go mod download
	@echo "✅ Development environment ready"

# Quick rebuild just the binary (for faster iteration)
quick:
	@echo "⚡ Quick rebuild (binary only)..."
	go build -o bin/devgru ./cmd/devgru
	@echo "✅ Quick build complete"
