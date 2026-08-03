GO ?= go

.PHONY: build test fmt vet check

build:
	$(GO) build -o bin/device-farm-server ./cmd/device-farm-server
	$(GO) build -o bin/device-host-agent ./cmd/device-host-agent
	$(GO) build -o bin/dafit-farm-harness ./cmd/dafit-farm-harness

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

check: fmt vet test build
