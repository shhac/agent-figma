BINARY := agent-figma
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOCACHE ?= $(CURDIR)/.cache/go-build

build:
	GOCACHE=$(GOCACHE) go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent-figma

test:
	GOCACHE=$(GOCACHE) go test ./... -count=1

test-short:
	GOCACHE=$(GOCACHE) go test ./... -count=1 -short

lint:
	golangci-lint run ./...

# Scoped to tracked files: this repo keeps a module cache under .cache/, which
# the go tool skips (dot-directory) but gofmt and goimports walk into, so a bare
# `-w .` rewrites vendored dependencies and makes `gofmt -l .` report noise.
fmt:
	gofmt -w $$(git ls-files '*.go')
	@command -v goimports >/dev/null && goimports -w $$(git ls-files '*.go') || echo "goimports not installed (optional; install: go install golang.org/x/tools/cmd/goimports@latest)"

clean:
	rm -f $(BINARY)
	rm -rf dist/

dev:
	GOCACHE=$(GOCACHE) go run ./cmd/agent-figma $(ARGS)

vet:
	GOCACHE=$(GOCACHE) go vet ./...

.PHONY: build test test-short lint fmt clean dev vet
