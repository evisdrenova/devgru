# DevGru VS Code Integration

This guide explains how to set up DevGru's VS Code integration for a seamless AI-powered development experience.

## Features

🚀 **Quick Launch**: `Cmd+Esc` (Mac) / `Ctrl+Esc` (Windows/Linux) to trigger DevGru
📝 **Selection Context**: Automatically sends your selected code to DevGru
🔍 **File References**: `Cmd+Opt+K` to insert `@file#L1-L10` references
📊 **Diagnostics**: Shares TypeScript/ESLint errors with DevGru for better context
🔄 **Live Diff Viewer**: See DevGru's proposed changes in VS Code's built-in diff viewer
🎯 **Auto-Connect**: Automatically detects when DevGru server starts

## Installation

### 1. Enable IDE Integration in DevGru

Add to your `devgru.yaml`:

```yaml
ide:
  enable: true
  transport: websocket
  diff_tool: auto
  port: 8123
```

### 2. Build and Install VS Code Extension

```bash
# Build the extension
chmod +x scripts/build-extension.sh
./scripts/build-extension.sh

# Install the extension
cd vscode-extension
code --install-extension devgru-code-*.vsix
```

### 3. Start DevGru IDE Server

```bash
# In your project directory
devgru ide connect
```

You should see:

```
###DEVGRU_VSCODE_HANDSHAKE###
DevGru IDE server starting on ws://127.0.0.1:8123/ws
🔌 VS Code extension should auto-detect and connect
```

## Usage

### Basic Workflow

1. **Start the server**: `devgru ide connect`
2. **Open VS Code** in your project
3. **Select code** you want DevGru to help with
4. **Press `Cmd+Esc`** to trigger DevGru
5. **Type your prompt** in the terminal
6. **Review results** in the beautiful TUI or diff viewer

### Key Bindings

| Shortcut                         | Action                |
| -------------------------------- | --------------------- |
| `Cmd+Esc` (Mac) / `Ctrl+Esc`     | Open DevGru panel     |
| `Cmd+Opt+K` (Mac) / `Ctrl+Alt+K` | Insert file reference |

### Commands

- **DevGru: Open Panel** - Trigger DevGru interface
- **DevGru: Insert File Reference** - Insert `@file#L1-L10` reference
- **DevGru: Run Prompt** - Quick prompt input

### Context Features

**Automatic Selection Context**: When you select code and trigger DevGru, the selection is automatically included in the prompt context.

**File References**: Use `Cmd+Opt+K` to insert references like:

- `@src/main.go` - entire file
- `@src/main.go#L23` - specific line
- `@src/main.go#L10-L20` - line range

**Diagnostics**: TypeScript/ESLint errors are automatically shared with DevGru to provide better context for fixes.

## Example Workflow

1. **Select a function** you want to optimize
2. **Press `Cmd+Esc`**
3. **Type**: "Optimize this function for performance and add error handling"
4. **DevGru runs** multiple AI workers with your selected code as context
5. **Review diff** in VS Code's diff viewer
6. **Accept or modify** the suggested changes

## Configuration

### VS Code Settings

```json
{
  "devgru.serverPort": 8123,
  "devgru.autoConnect": true,
  "devgru.enableDiagnostics": true
}
```

### DevGru Config

```yaml
ide:
  enable: true # Enable IDE integration
  transport: websocket # Communication method
  diff_tool: auto # Diff display method
  port: 8123 # WebSocket port
```

## Troubleshooting

### Extension Not Connecting

1. Check if DevGru server is running: `devgru ide status`
2. Restart VS Code
3. Check the port is not in use: `lsof -i :8123`

### No Context Being Sent

1. Make sure you have text selected
2. Check VS Code Developer Console for errors
3. Verify `devgru.enableDiagnostics` is true

### Diff Viewer Not Working

1. Ensure `ide.diff_tool: auto` in config
2. Check that files are in workspace
3. Try manual diff with `git diff`

## Development

To develop the extension:

```bash
cd vscode-extension
npm install
npm run watch
```

Then press `F5` in VS Code to launch a new Extension Development Host.

## Architecture

```
┌─────────────┐  WebSocket  ┌──────────────┐
│ VS Code     │ ←────────→  │ DevGru CLI   │
│ Extension   │   JSON-RPC  │ (Go Server)  │
└─────────────┘             └──────────────┘
      ↑                            ↑
   Selection,                 Multi-agent
   Diagnostics,               LLM Workers
   File Events                & Consensus
```

The extension communicates with DevGru via WebSocket, sending context and receiving diffs/responses in real-time.
