.PHONY: build run clean install test fmt vet check build-all dev dev-tools app pkg dmg macos

# Binary name
BINARY_NAME=sushi

# Build the application
build:
	go build -o $(BINARY_NAME) main.go

# Run the application
run:
	go run main.go

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)

# Install the application
install:
	go install

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run all checks
check: fmt vet test

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_NAME)-linux-amd64 main.go
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -o bin/$(BINARY_NAME)-darwin-arm64 main.go
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_NAME)-windows-amd64.exe main.go

# Development mode with auto-reload (requires air)
dev:
	air

# Install development tools
dev-tools:
	go install github.com/cosmtrek/air@latest


# macOS app, installer and disk image (needs Xcode). See macos/scripts/build.sh.
VERSION ?= 0.1.0

# Sushi.app: a window around sushi, with its own icon and font
app:
	VERSION=$(VERSION) macos/scripts/build.sh app

# Installer that puts Sushi.app in /Applications and sushi in /usr/local/bin
pkg:
	VERSION=$(VERSION) macos/scripts/build.sh pkg

# Disk image to drag Sushi.app to Applications from
dmg:
	VERSION=$(VERSION) macos/scripts/build.sh dmg

# All three, into dist/
macos:
	VERSION=$(VERSION) macos/scripts/build.sh all
