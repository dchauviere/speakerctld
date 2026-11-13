.PHONY: all build test test-race test-coverage clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# CGO flags
CGO_CFLAGS=-Ic_wrapper
CGO_LDFLAGS=-lswupdate

all: build

build:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOBUILD) -v .

# Run tests
test:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOTEST) -v ./...

# Run tests with race detector
test-race:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOTEST) -race -v ./...

# Run tests with coverage
test-coverage:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	$(GOTEST) -coverprofile=coverage.out -v ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Clean up
clean:
	rm -f coverage.out coverage.html

# Run go mod download
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Build the package (default)"
	@echo "  build            - Build the package"
	@echo "  test             - Run tests"
	@echo "  test-race        - Run tests with race detector"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  clean            - Clean up"
	@echo "  deps             - Download dependencies"
