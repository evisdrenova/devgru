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

# Ensure .vscodeignore exists with correct content
echo "📝 Creating .vscodeignore..."
cat > .vscodeignore << 'EOF'
.vscode/**
.vscode-test/**
src/**
.gitignore
.yarnrc
vsc-extension-quickstart.md
**/tsconfig.json
**/.eslintrc.json
**/*.map
**/*.ts
node_modules/**
../**
!out/**
EOF

# Ensure LICENSE exists
if [ ! -f "LICENSE" ]; then
    echo "📄 Creating LICENSE file..."
    cat > LICENSE << 'EOF'
MIT License

Copyright (c) 2025 DevGru

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
EOF
fi

# Remove any existing .vsix files
rm -f *.vsix

# Package the extension
echo "📦 Packaging extension..."
vsce package

echo "✅ Extension built successfully!"
if ls *.vsix 1> /dev/null 2>&1; then
    VSIX_FILE=$(ls -t *.vsix | head -1)
    echo "📁 VSIX file created: $VSIX_FILE"
    echo ""
    echo "To install:"
    echo "  code --install-extension $VSIX_FILE"
    echo ""
    echo "Or install via make:"
    echo "  make install-extension"
else
    echo "❌ No VSIX file found. Check for errors above."
    exit 1
fi