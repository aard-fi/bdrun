.PHONY: all clean proto socket research relay test deps build-all

# Variables
PROTO_DIR := proto
PROTO_FILES := $(wildcard $(PROTO_DIR)/*.proto)
PROTO_GO := $(patsubst $(PROTO_DIR)/%.proto,internal/protocol/%.pb.go,$(PROTO_FILES))
GOPATH_BIN := $(shell go env GOPATH)/bin
export PATH := $(PATH):$(GOPATH_BIN)

# Version information
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

# Build targets for different platforms
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64
BINARIES := bdrun-socket bdrun-research bdrun-relay

# Default target - build for current platform
all: proto socket

# Install dependencies
deps:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go mod download

# Generate protobuf code
proto: $(PROTO_GO)

internal/protocol/%.pb.go: proto/%.proto
	protoc --go_out=. --go_opt=paths=source_relative $<
	mv proto/*.pb.go internal/protocol/ || true

# Build for current platform (native)
socket: proto
	go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket ./cmd/bdrun-socket

research: proto
	go build -tags research -ldflags "$(LDFLAGS)" -o bin/bdrun-research ./cmd/bdrun-research

relay: proto
	go build -ldflags "$(LDFLAGS)" -o bin/bdrun-relay ./cmd/bdrun-relay

# Build for all platforms (currently only socket is implemented)
build-all: proto
	@echo "Building for all platforms..."
	@$(MAKE) build-socket-all
	@echo "Build complete! Binaries in bin/"
	@echo ""
	@echo "Note: bdrun-research and bdrun-relay are not yet implemented"
	@echo "      Use 'make build-research-all' and 'make build-relay-all' when ready"

# Build socket binary for all platforms
build-socket-all: proto
	@echo "Building bdrun-socket for all platforms..."
	GOOS=linux GOARCH=amd64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-linux-amd64 ./cmd/bdrun-socket
	GOOS=linux GOARCH=arm64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-linux-arm64 ./cmd/bdrun-socket
	GOOS=darwin GOARCH=amd64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-darwin-amd64 ./cmd/bdrun-socket
	GOOS=darwin GOARCH=arm64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-darwin-arm64 ./cmd/bdrun-socket

# Build research binary for all platforms
build-research-all: proto
	@echo "Building bdrun-research for all platforms..."
	GOOS=linux GOARCH=amd64 go build -tags research -ldflags "$(LDFLAGS)" -o bin/bdrun-research-linux-amd64 ./cmd/bdrun-research
	GOOS=linux GOARCH=arm64 go build -tags research -ldflags "$(LDFLAGS)" -o bin/bdrun-research-linux-arm64 ./cmd/bdrun-research
	GOOS=darwin GOARCH=amd64 go build -tags research -ldflags "$(LDFLAGS)" -o bin/bdrun-research-darwin-amd64 ./cmd/bdrun-research
	GOOS=darwin GOARCH=arm64 go build -tags research -ldflags "$(LDFLAGS)" -o bin/bdrun-research-darwin-arm64 ./cmd/bdrun-research

# Build relay binary for all platforms
build-relay-all: proto
	@echo "Building bdrun-relay for all platforms..."
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/bdrun-relay-linux-amd64 ./cmd/bdrun-relay
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/bdrun-relay-linux-arm64 ./cmd/bdrun-relay
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/bdrun-relay-darwin-amd64 ./cmd/bdrun-relay
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/bdrun-relay-darwin-arm64 ./cmd/bdrun-relay

# Build specific platform combinations
build-linux-amd64: proto
	GOOS=linux GOARCH=amd64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-linux-amd64 ./cmd/bdrun-socket

build-linux-arm64: proto
	GOOS=linux GOARCH=arm64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-linux-arm64 ./cmd/bdrun-socket

build-darwin-amd64: proto
	GOOS=darwin GOARCH=amd64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-darwin-amd64 ./cmd/bdrun-socket

build-darwin-arm64: proto
	GOOS=darwin GOARCH=arm64 go build -tags socket -ldflags "$(LDFLAGS)" -o bin/bdrun-socket-darwin-arm64 ./cmd/bdrun-socket

# Build with embedded config (current platform only)
socket-embedded: proto
	go build -tags socket -ldflags "$(LDFLAGS) -X main.embeddedConfig=$$(cat configs/daemon-production.yaml | base64)" \
		-o bin/bdrun-socket-embedded ./cmd/bdrun-socket

# Run tests (native platform only)
test:
	@echo "Running tests on native platform..."
	go test -v ./...

# Run tests with coverage (native platform only)
test-coverage:
	@echo "Running tests with coverage on native platform..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run short tests only (skip integration tests)
test-short:
	go test -v -short ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run ./...

# Show build information
info:
	@echo "Build Information:"
	@echo "  Version:    $(VERSION)"
	@echo "  Commit:     $(COMMIT)"
	@echo "  Build Time: $(BUILD_TIME)"
	@echo "  Go Version: $(shell go version)"
	@echo ""
	@echo "Target Platforms:"
	@echo "  - linux/amd64 (x86_64)"
	@echo "  - linux/arm64 (aarch64)"
	@echo "  - darwin/amd64 (Intel Mac)"
	@echo "  - darwin/arm64 (Apple Silicon)"

# Show help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build Targets:"
	@echo "  all              - Build default binaries for current platform"
	@echo "  build-all        - Build all binaries for all platforms"
	@echo "  socket           - Build bdrun-socket for current platform"
	@echo "  research         - Build bdrun-research for current platform"
	@echo "  relay            - Build bdrun-relay for current platform"
	@echo "  socket-embedded  - Build socket binary with embedded config"
	@echo ""
	@echo "Cross-Platform Builds:"
	@echo "  build-socket-all    - Build bdrun-socket for all platforms"
	@echo "  build-research-all  - Build bdrun-research for all platforms"
	@echo "  build-relay-all     - Build bdrun-relay for all platforms"
	@echo "  build-linux-amd64   - Build for Linux x86_64"
	@echo "  build-linux-arm64   - Build for Linux ARM64"
	@echo "  build-darwin-amd64  - Build for macOS Intel"
	@echo "  build-darwin-arm64  - Build for macOS Apple Silicon"
	@echo ""
	@echo "Testing:"
	@echo "  test             - Run all tests (native platform only)"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  test-short       - Run short tests (skip integration tests)"
	@echo ""
	@echo "Development:"
	@echo "  deps             - Install development dependencies"
	@echo "  proto            - Generate protobuf code"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  clean            - Remove build artifacts"
	@echo "  info             - Show build information"
	@echo "  help             - Show this help message"
