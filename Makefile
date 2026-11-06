# statusline — build, test and release helpers.

BIN := statusline
ifeq ($(OS),Windows_NT)
BIN := statusline.exe
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet fmt clean cross

all: vet test build

## build: native binary for this machine (no extension on unix, .exe on windows)
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/statusline
	@echo "built $(BIN) $(VERSION)"

## test: unit + golden + contract tests
test:
	go test ./...

## vet: static checks and formatting
vet:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

## cross: all release artifacts
cross:
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-linux-x86_64 ./cmd/statusline
	GOOS=linux   GOARCH=386   go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-linux-x86 ./cmd/statusline
	GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-darwin-x86_64 ./cmd/statusline
	GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-darwin-aarch64 ./cmd/statusline
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-windows-x86_64.exe ./cmd/statusline
	GOOS=windows GOARCH=386   go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statusline-windows-x86.exe ./cmd/statusline

## fmt: format the tree
fmt:
	gofmt -w .

clean:
	rm -rf dist $(BIN)
