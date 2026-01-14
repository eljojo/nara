.PHONY: all build test run run2 clean build-nix test-v test-fast lint-report build-web watch-web build-backup

# Default target: build and test
all: build test

# Build web assets (JS + CSS bundles)
# Bundles Preact, D3, dayjs - no CDN dependencies
build-web:
	@echo "Building web assets..."
	@npm run build
	@./node_modules/.bin/astro build --root docs --config astro.config.mjs
	@rm -rf nara-web/public/docs
	@mkdir -p nara-web/public/docs
	@cp -R docs/dist/. nara-web/public/docs
	@echo "✓ Built nara-web/public/app.js, app.css, vendor.css, docs/"

# Watch web assets for changes (dev mode)
watch-web:
	@echo "Watching web assets for changes..."
	@npm run watch

# Build the nara binary (depends on web assets)
build: build-web
	@echo "Building nara..."
	@mkdir -p bin
	@go build -mod=mod -o bin/nara cmd/nara/main.go
	@echo "✓ Built bin/nara"

# Build the nara-backup tool (no web assets needed)
build-backup:
	@echo "Building nara-backup..."
	@mkdir -p bin
	@go build -mod=mod -o bin/nara-backup cmd/nara-backup/*.go
	@echo "✓ Built bin/nara-backup"

# Run all tests (includes slow integration tests)
test: lint-report
	@echo "Running all tests..."
	@go test ./... -timeout 3m

# Run tests with verbose output
test-v:
	@echo "Running tests with verbose output..."
	@go test -v ./... -timeout 3m

# Run only fast tests (skip slow integration tests)
test-fast:
	@echo "Running fast tests (skipping integration tests)..."
	@go test -short ./... -timeout 3m

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
	@rm -f nara-web/public/app.js nara-web/public/app.js.map nara-web/public/app.css nara-web/public/vendor.css
	@rm -rf nara-web/public/docs
	@rm -f nara-web/src/generated/iconoir.css
	@echo "✓ Cleaned"

# Build using Nix
build-nix:
	@echo "Building nara using Nix..."
	@nix build .#nara --option substituters https://cache.nixos.org
	@echo "✓ Built nara using Nix"

lint-report:
	@golangci-lint run
