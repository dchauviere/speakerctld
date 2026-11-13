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

# SWUpdate paths (can be overridden)
SWUPDATE_CTRL_SOCKET?=/tmp/sockinstctrl
SWUPDATE_PROG_SOCKET?=/tmp/swupdateprog

# CGO flags for SWUpdate integration
CGO_CFLAGS=-Iinternal/swupdate/c_wrapper
CGO_LDFLAGS=-lswupdate

all: build

build: deps
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o bin/$(BINARY) ./$(SRC_DIR)

# Build with debug information
debug: deps
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o bin/$(BINARY) -v ./$(SRC_DIR)

# Run tests
test: deps
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage: deps
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Run tests with race detector
test-race: deps
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
install: deps
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o /usr/local/bin/$(BINARY) ./$(SRC_DIR)

# Run the application
run: deps
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o /tmp/$(BINARY) && /tmp/$(BINARY)

# Build for Linux ARM (Raspberry Pi) with CGO
build-arm: deps
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o bin/$(BINARY)-arm ./$(SRC_DIR)

# Build for Linux ARM64 with CGO
build-arm64: deps
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o bin/$(BINARY)-arm64 ./$(SRC_DIR)

# Build for local development with SWUpdate socket paths
build-dev: deps
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o bin/$(BINARY) -ldflags="-X main.DefaultCtrlSocketPath=$(SWUPDATE_CTRL_SOCKET) -X main.DefaultProgSocketPath=$(SWUPDATE_PROG_SOCKET)" ./$(SRC_DIR)

# Pre-commit hooks installation (requires pre-commit package: pip install pre-commit)
pre-commit-install:
	pre-commit install

pre-commit-run:
	pre-commit run --all-files

# Build and run with data directory
run-dev:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -o /tmp/$(BINARY) && /tmp/$(BINARY) -data-dir /tmp/speakerctld-data

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Build the project (default)"
	@echo "  build            - Build the project with CGO"
	@echo "  debug            - Build with debug information"
	@echo "  test             - Run all tests"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  test-race        - Run tests with race detector"
	@echo "  lint             - Run linter"
	@echo "  deps             - Download and tidy dependencies"
	@echo "  clean            - Clean up build artifacts"
	@echo "  install          - Install binary to /usr/local/bin"
	@echo "  run              - Build and run"
	@echo "  build-arm        - Build for Linux ARM with CGO"
	@echo "  build-arm64      - Build for Linux ARM64 with CGO"
	@echo "  build-dev        - Build for development"
	@echo "  run-dev          - Build and run for development"
	@echo "  pre-commit-install - Install pre-commit hooks"
	@echo "  pre-commit-run    - Run pre-commit checks on all files"
