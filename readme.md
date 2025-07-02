# DevGru 🎯

**Multi-Agent Consensus CLI with VS Code Integration**

DevGru is a powerful command-line tool that runs prompts through multiple LLM "workers," uses "judge" models to critique responses, and returns a consensus result. Built with Go and Bubble Tea for beautiful terminal UIs, plus seamless VS Code integration.

## ✨ Features

- 🤖 **Multi-Agent Workers**: Run prompts through multiple LLMs simultaneously
- ⚖️ **Judge-Based Consensus**: AI judges evaluate and score responses
- 🎨 **Beautiful TUI**: Interactive terminal interface with collapsible sections
- 🔌 **VS Code Integration**: Seamless IDE experience with context awareness
- 📊 **Cost Tracking**: Real-time token usage and API cost estimation
- 🚀 **Streaming Responses**: Live token streaming for all providers
- ⚡ **Parallel Execution**: Workers and judges run concurrently
- 💾 **Response Caching**: BoltDB storage for reproducible results

## 🏗️ Architecture

```
DevGru Monorepo
├── 🐹 Go CLI (Bubble Tea TUI)
│   ├── Multi-agent workers
│   ├── Judge-based consensus
│   └── WebSocket IDE server
└── 🆚 VS Code Extension (TypeScript)
    ├── Selection context sharing
    ├── Live diff viewer
    └── Auto-handshake detection
```

## 🚀 Quick Start

### Prerequisites

- **Go 1.19+**
- **Node.js 16+** (for VS Code extension)
- **VS Code** (optional, for IDE integration)

### Installation

```bash
# Clone the repository
git clone https://github.com/evisdrenova/devgru.git
cd devgru

# Install dependencies
make install

# Build everything
make build

# Set up your API keys
export OPENAI_API_KEY=your_key_here
export ANTHROPIC_API_KEY=your_key_here

# Copy example config
cp examples/devgru.yaml ./devgru.yaml
```

### Basic Usage

```bash
# Run a simple prompt
./bin/devgru run "Explain quantum computing in simple terms"

# Start IDE integration server
./bin/devgru ide connect

# Check status
./bin/devgru ide status
```

## 📝 Configuration

Create a `devgru.yaml` file:

```yaml
providers:
  openai:
    kind: openai
    model: gpt-4o-mini
    base_url: https://api.openai.com/v1

workers:
  - id: creative
    provider: openai
    temperature: 0.8
    system_prompt: "You are a creative assistant."

  - id: analytical
    provider: openai
    temperature: 0.2
    system_prompt: "You are an analytical assistant."

judges:
  - id: quality-judge
    provider: openai
    system_prompt: |
      Grade responses 0-10 for accuracy and clarity.
      Respond with: {"score": <int>, "reason": "<text>"}

consensus:
  algorithm: score_top1 # or "majority"
  min_score: 6
  timeout: 45s

ide:
  enable: true
  port: 8123
```

## 🆚 VS Code Integration

### Installation

```bash
# Package the extension
make package-extension

# Install in VS Code
make install-extension
```

### Usage

1. **Start DevGru IDE server**: `make run-ide`
2. **Open VS Code** in your project
3. **Select code** you want help with
4. **Press `Cmd+Esc`** (Mac) or `Ctrl+Esc` (Windows/Linux)
5. **Type your prompt** and see consensus results!

### Key Features

- 🎯 **Quick Launch**: `Cmd+Esc` triggers DevGru
- 📝 **Auto Context**: Selected code automatically included
- 🔗 **File References**: `Cmd+Opt+K` inserts `@file#L1-L10`
- 🐛 **Diagnostics**: TypeScript/ESLint errors shared with DevGru
- 🔄 **Live Diffs**: See proposed changes in VS Code's diff viewer

## 🛠️ Development

### Monorepo Commands

```bash
# Development workflow
make dev              # Start development mode
make test             # Run all tests
make lint             # Run linters
make format           # Format code

# Building
make build            # Build everything
make build-go         # Build CLI only
make build-extension  # Build extension only

# Utilities
make clean            # Clean build artifacts
make help             # Show all commands
```

### Project Structure

```
devgru/
├── cmd/devgru/          # CLI entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── provider/        # LLM provider interfaces
│   │   ├── openai/      # OpenAI implementation
│   │   └── factories/   # Provider factory
│   ├── runner/          # Multi-agent orchestration
│   │   ├── types.go     # Core types
│   │   ├── runner.go    # Main runner logic
│   │   ├── consensus.go # Consensus algorithms
│   │   └── judge.go     # Judge evaluation
│   └── ide/             # VS Code integration
├── ui/                  # Bubble Tea TUI components
├── vscode-extension/    # VS Code extension (TypeScript)
├── scripts/             # Build and utility scripts
└── examples/            # Example configurations
```

### Adding New Providers

1. **Implement the Provider interface** in `internal/provider/`
2. **Add to factory** in `internal/provider/factories/factory.go`
3. **Update config validation** in `internal/config/config.go`
4. **Add example config** in `examples/devgru.yaml`

### Adding New Consensus Algorithms

1. **Add algorithm** to `internal/runner/consensus.go`
2. **Update switch statement** in `runConsensus()`
3. **Add configuration options** if needed
4. **Write tests** for the new algorithm

## 🧪 Testing

```bash
# Run all tests
make test

# Test individual components
make test-go           # Go tests only
make test-extension    # Extension tests only

# Run with coverage
go test -cover ./...
```

## 📊 Consensus Algorithms

- **`majority`**: Simple majority voting (currently first successful)
- **`score_top1`**: Judge-based scoring, highest score wins
- **`embedding_cluster`**: Group similar responses (TODO)
- **`referee`**: LLM referee picks best response (TODO)

## 🔌 Supported Providers

- ✅ **OpenAI** (GPT-4, GPT-3.5, etc.)
- 🔄 **Anthropic** (Claude, in progress)
- 🔄 **Ollama** (Local models, in progress)

## 📈 Roadmap

- [ ] **Anthropic Provider**: Full Claude support
- [ ] **Ollama Provider**: Local model support
- [ ] **Embedding Clustering**: Similarity-based consensus
- [ ] **Referee Algorithm**: LLM-based consensus
- [ ] **Response Caching**: BoltDB integration
- [ ] **Web UI**: Browser-based interface
- [ ] **Plugins**: Extensible provider system

## 🤝 Contributing

1. **Fork the repository**
2. **Create a feature branch**: `git checkout -b feature/amazing-feature`
3. **Make your changes** and add tests
4. **Run the test suite**: `make test`
5. **Submit a pull request**

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by multi-agent AI research
- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for beautiful TUIs
- VS Code integration inspired by [Claude Code](https://claude.ai/chat)

---

**DevGru** - _Where multiple AI minds reach consensus_ 🎯
