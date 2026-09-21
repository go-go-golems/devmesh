.PHONY: all lint lintmax golangci-lint-install glazed-lint-build glazed-lint test test-race vet build build-bin clean fmt tidy docker-lint gosec govulncheck logcopter-generate logcopter-check goreleaser release install bump-go-go-golems

BINARY ?= devmesh
DAEMON_BINARY ?= devmeshd
MODULE ?= github.com/go-go-golems/devmesh
GO_PACKAGES ?= ./...
LINT_PACKAGES ?= ./cmd/... ./internal/... ./pkg/... ./integration/... ./examples/native-go
LOGCOPTER_PACKAGES ?= ./cmd/... ./internal/... ./pkg/...

GORELEASER_ARGS ?= --skip=sign --snapshot --clean
GORELEASER_TARGET ?= --single-target
GOLANGCI_LINT_VERSION ?= $(shell cat .golangci-lint-version)
GOLANGCI_LINT_BIN ?= $(CURDIR)/.bin/golangci-lint
GLAZED_LINT_BIN ?= /tmp/glazed-lint
GLAZED_LINT_PKG ?= github.com/go-go-golems/glazed/cmd/tools/glazed-lint
GLAZED_VERSION ?= $(shell GOWORK=off go list -m -f '{{.Version}}' github.com/go-go-golems/glazed 2>/dev/null)
GLAZED_LINT_DIRS ?= ./cmd/... ./internal/... ./pkg/...
GLAZED_LINT_FLAGS ?=

all: lint test build

docker-lint:
	docker run --rm -v $(shell pwd):/app -w /app golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run -v $(LINT_PACKAGES)

golangci-lint-install:
	mkdir -p $(dir $(GOLANGCI_LINT_BIN))
	GOBIN=$(dir $(GOLANGCI_LINT_BIN)) GOWORK=off go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

glazed-lint-build:
	@echo "Building glazed-lint from the selected Glazed module..."
	@if [ -n "$(GLAZED_VERSION)" ] && [ "$(GLAZED_VERSION)" != "(devel)" ]; then \
		echo "Installing $(GLAZED_LINT_PKG)@$(GLAZED_VERSION)"; \
		GOBIN=$(dir $(GLAZED_LINT_BIN)) GOWORK=off go install $(GLAZED_LINT_PKG)@$(GLAZED_VERSION); \
	else \
		echo "Installing $(GLAZED_LINT_PKG) from workspace/module"; \
		GOBIN=$(dir $(GLAZED_LINT_BIN)) GOWORK=off go install $(GLAZED_LINT_PKG); \
	fi

glazed-lint: glazed-lint-build
	GOWORK=off go vet -vettool=$(GLAZED_LINT_BIN) $(GLAZED_LINT_FLAGS) $(GLAZED_LINT_DIRS)

lint: golangci-lint-install glazed-lint-build
	$(GOLANGCI_LINT_BIN) run -v $(LINT_PACKAGES)
	GOWORK=off go vet -vettool=$(GLAZED_LINT_BIN) $(GLAZED_LINT_FLAGS) $(GLAZED_LINT_DIRS)

lintmax: golangci-lint-install glazed-lint-build
	$(GOLANGCI_LINT_BIN) run -v --max-same-issues=100 $(LINT_PACKAGES)
	GOWORK=off go vet -vettool=$(GLAZED_LINT_BIN) $(GLAZED_LINT_FLAGS) $(GLAZED_LINT_DIRS)

test:
	GOWORK=off go test $(GO_PACKAGES) -count=1

test-race:
	GOWORK=off go test -race $(GO_PACKAGES) -count=1

vet:
	GOWORK=off go vet $(GO_PACKAGES)

build:
	GOWORK=off go generate ./...
	GOWORK=off go build $(GO_PACKAGES)

build-bin:
	mkdir -p ./dist
	GOWORK=off go build -o ./dist/$(BINARY) ./cmd/$(BINARY)
	GOWORK=off go build -o ./dist/$(DAEMON_BINARY) ./cmd/$(DAEMON_BINARY)

clean:
	rm -rf ./dist ./.bin

fmt:
	gofmt -w $$(git ls-files '*.go')

tidy:
	GOWORK=off go mod tidy

gosec:
	GOWORK=off go install github.com/securego/gosec/v2/cmd/gosec@latest
	gosec -exclude-generated -exclude=G101,G304,G301,G306,G204 -exclude-dir=.history ./...

govulncheck:
	GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

logcopter-generate:
	GOWORK=off go generate ./...

logcopter-check:
	GOWORK=off go tool logcopter-gen -area-prefix go-go-golems.devmesh -strip-prefix $(MODULE) -check $(LOGCOPTER_PACKAGES)

goreleaser:
	GOWORK=off goreleaser release $(GORELEASER_ARGS) $(GORELEASER_TARGET)

release:
	git push origin --tags
	GOWORK=off GOPROXY=proxy.golang.org go list -m $(MODULE)@$(shell svu current)

install:
	GOWORK=off go install ./cmd/$(BINARY) ./cmd/$(DAEMON_BINARY)

bump-go-go-golems:
	@deps="$$(awk '/^require[[:space:]]+github\.com\/go-go-golems\// { print $$2 } /^[[:space:]]*github\.com\/go-go-golems\// { print $$1 }' go.mod | sort -u)"; \
	if [ -z "$$deps" ]; then \
		echo "No github.com/go-go-golems dependencies in go.mod"; \
	else \
		echo "Bumping go-go-golems dependencies:"; \
		echo "$$deps"; \
		for dep in $$deps; do GOWORK=off go get "$${dep}@latest"; done; \
	fi
	GOWORK=off go mod tidy
