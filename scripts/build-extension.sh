#!/usr/bin/env bash
set -e
cd vscode-extension

echo "📦 Installing dependencies…"
npm install

echo "🔧 Compiling TypeScript…"
npm run compile

echo "🔨 Bundling into single extension.js via esbuild…"

echo "📝 Writing .vscodeignore…"
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

# we bundle all deps into out/extension.js
node_modules/**
../**
!out/**
EOF

echo "📦 Packaging extension…"
vsce package
