#!/bin/bash

# Sushi Project Setup Script

echo "🍣 Setting up Sushi project..."

# Create directory structure
echo "Creating directories..."
mkdir -p cmd/sushi
mkdir -p internal/{app,fs,ui/components,config,utils}
mkdir -p configs docs tests/testdata
mkdir -p bin

# Initialize go module
echo "Initializing Go module..."
go mod init github.com/icichainz/sushi

# Install dependencies
echo "Installing dependencies..."
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles

# Tidy up
go mod tidy

echo "✅ Project structure created!"
echo ""
echo "Next steps:"
echo "  1. Update the module path in go.mod to your GitHub username"
echo "  2. Update the import paths in all .go files"
echo "  3. Run 'make run' or 'go run main.go' to start"
echo ""
echo "Happy coding! 🚀"