.PHONY: build run clean install test fmt vet

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


# macOS PKG installer
PKG_NAME = $(BINARY_NAME).pkg
INSTALL_PATH = /usr/local/bin

build-darwin-universal:
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -o bin/$(BINARY_NAME)-arm64 main.go
	lipo -create -output bin/$(BINARY_NAME) bin/$(BINARY_NAME)-amd64 bin/$(BINARY_NAME)-arm64
	rm -f bin/$(BINARY_NAME)-amd64 bin/$(BINARY_NAME)-arm64

pkg: build-darwin-universal
	@mkdir -p pkg-root$(INSTALL_PATH)
	cp bin/$(BINARY_NAME) pkg-root$(INSTALL_PATH)/$(BINARY_NAME)
	chmod +x pkg-root$(INSTALL_PATH)/$(BINARY_NAME)
	pkgbuild \
		--identifier "com.icichainz.$(BINARY_NAME)" \
		--version "0.0.1" \
		--root pkg-root \
		--install-location "/" \
		$(PKG_NAME)
	@rm -rf pkg-root
	@echo "✅ PKG installer created: $(PKG_NAME)"