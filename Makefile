.PHONY: all build test run run2 clean build-nix test-v test-fast lint-report

# Default target: build and test
all: build test

# Build the nara binary
build:
	@echo "Building nara..."
	@mkdir -p bin
	@go build -mod=mod -o bin/nara cmd/nara/main.go
	@echo "✓ Built bin/nara"

# Run all tests (includes slow integration tests)
test:
	@echo "Running all tests..."
	@go test ./... -timeout 2m

# Run tests with verbose output
test-v:
	@echo "Running tests with verbose output..."
	@go test -v ./... -timeout 2m

# Run only fast tests (skip slow integration tests)
test-fast:
	@echo "Running fast tests (skipping integration tests)..."
	@go test -short ./... -timeout 2m

# Run nara with web UI on port 8080
run: build
	@echo "Starting nara on :8080..."
	@./bin/nara -serve-ui -http-addr :8080 -verbose

# Run second instance with nara-id nixos on port 8081
run2: build
	@echo "Starting nara (nixos) on :8081..."
	@./bin/nara -serve-ui -http-addr :8081 -nara-id nixos -verbose

run3: build
	@echo "Starting nara (gossip-ghost) on :8081..."
	@./bin/nara -serve-ui -http-addr :8082 -nara-id gossip-ghost -verbose -transport gossip

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@echo "✓ Cleaned"

# Build using Nix
build-nix:
	@echo "Building nara using Nix..."
	@nix build .#nara
	@echo "✓ Built nara using Nix"

lint-report:
	@golangci-lint run
