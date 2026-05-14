.PHONY: build test clean release lint install

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.baseImageTag=$(VERSION)

# Build the binary
build:
	go build -ldflags "$(LDFLAGS)" -o oss-back2base .

# Run all tests
test:
	go test -v ./...

# Remove build artifacts
clean:
	rm -f oss-back2base
	rm -rf dist/

# Local release (dry-run)
release:
	goreleaser release --snapshot --clean

# Lint
lint:
	go vet ./...

# Install locally (for development)
install: build
	mkdir -p $(HOME)/.local/bin
	cp oss-back2base $(HOME)/.local/bin/oss-back2base
