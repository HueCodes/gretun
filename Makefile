BINARY    := gretun
MODULE    := github.com/HueCodes/gretun
BUILD_DIR := bin

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -ldflags "\
  -X $(MODULE)/internal/version.Version=$(VERSION) \
  -X $(MODULE)/internal/version.Commit=$(COMMIT) \
  -X $(MODULE)/internal/version.BuildTime=$(BUILD_TIME)"

.PHONY: build test vet lint cover clean

build:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/gretun

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

clean:
	rm -rf $(BUILD_DIR) coverage.out
