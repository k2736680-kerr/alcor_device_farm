GO ?= go

.PHONY: build test fmt vet check console

# The server embeds internal/consoleui/dist (//go:embed), so the frontend must
# be built before the Go build. Requires the console dependencies (pnpm).
console:
	cd console && pnpm build

build: console
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
