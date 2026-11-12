.PHONY: all build test clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary name
BINARY=speakerctld

# Source files
SRC_DIR=cmd/speakerctld

all: build

build:
	$(GOBUILD) -o bin/$(BINARY) ./$(SRC_DIR)

# Build with debug information
debug:
	$(GOBUILD) -o bin/$(BINARY) -v ./$(SRC_DIR)

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Run tests with race detector
test-race:
	$(GOTEST) -v -race ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

# Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Clean up
clean:
	rm -rf bin/ coverage.out coverage.html

# Install the binary
install:
	$(GOBUILD) -o /usr/local/bin/$(BINARY) ./$(SRC_DIR)

# Run the application
run:
	$(GOBUILD) -o /tmp/$(BINARY) && /tmp/$(BINARY)

# Build for Linux ARM (Raspberry Pi)
build-arm:
	GOOS=linux GOARCH=arm $(GOBUILD) -o bin/$(BINARY)-arm ./$(SRC_DIR)

# Build for Linux ARM64
build-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o bin/$(BINARY)-arm64 ./$(SRC_DIR)

# Show help
help:
	@echo "Available targets:"
	@echo "  all           - Build the project (default)"
	@echo "  build         - Build the project"
	@echo "  debug         - Build with debug information"
	@echo "  test          - Run all tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  test-race     - Run tests with race detector"
	@echo "  lint          - Run linter"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  clean         - Clean up build artifacts"
	@echo "  install       - Install binary to /usr/local/bin"
	@echo "  run           - Build and run"
	@echo "  build-arm     - Build for Linux ARM"
	@echo "  build-arm64   - Build for Linux ARM64"
