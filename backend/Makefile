.PHONY: build generate test test-unit test-integration test-e2e

UNIT_PACKAGES := ./internal/... ./ent/... ./migrations ./cmd/...

INTEGRATION_PACKAGES := \
	./internal/repository \
	./internal/middleware \
	./internal/server/routes \
	./internal/pkg/tlsfingerprint \
	./ent/migrate \
	./migrations \
	./cmd/...

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test $(UNIT_PACKAGES)
	golangci-lint run ./...

test-unit:
	go test -tags=unit $(UNIT_PACKAGES)

test-integration:
	go test -p 1 -tags=integration $(INTEGRATION_PACKAGES)

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
