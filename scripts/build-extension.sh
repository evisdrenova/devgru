#!/bin/bash

set -e

echo "🔨 Building DevGru VS Code Extension"

# Check if we're in the right directory
if [ ! -f "vscode-extension/package.json" ]; then
    echo "❌ Error: Run this script from the project root directory"
    exit 1
fi

cd vscode-extension

# Install dependencies
echo "📦 Installing dependencies..."
npm install

# Compile TypeScript
echo "🔧 Compiling TypeScript..."
npm run compile

# Install vsce if not present
if ! command -v vsce &> /dev/null; then
    echo "📦 Installing vsce..."
    npm install -g @vscode/vsce
fi

# Package the extension
echo "📦 Packaging extension..."
vsce package

echo "✅ Extension built successfully!"
echo "📁 VSIX file created: $(ls -la *.vsix | tail -1 | awk '{print $9}')"
echo ""
echo "To install:"
echo "  code --install-extension $(ls *.vsix | tail -1)"
echo ""
echo "Or publish to marketplace:"
echo "  vsce publish"